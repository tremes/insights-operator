package controller

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift/api/config/v1alpha1"
	v1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	"github.com/openshift/insights-operator/pkg/anonymization"
	"github.com/openshift/insights-operator/pkg/authorizer/clusterauthorizer"
	"github.com/openshift/insights-operator/pkg/config"
	"github.com/openshift/insights-operator/pkg/config/configobserver"
	"github.com/openshift/insights-operator/pkg/gather"
	"github.com/openshift/insights-operator/pkg/insights/insightsclient"
	"github.com/openshift/insights-operator/pkg/insights/insightsreport"
	"github.com/openshift/insights-operator/pkg/insights/insightsuploader"
	"github.com/openshift/insights-operator/pkg/recorder"
	"github.com/openshift/insights-operator/pkg/recorder/diskrecorder"
)

const (
	DataGatheredCondition = "DataGathered"
	// NoDataGathered is a reason when there is no data gathered - e.g the resource is not in a cluster
	NoDataGatheredReason = "NoData"
	// Error is a reason when there is some error and no data gathered
	GatherErrorReason = "GatherError"
	// Panic is a reason when there is some error and no data gathered
	GatherPanicReason = "GatherPanic"
	// GatheredOK is a reason when data is gathered as expected
	GatheredOKReason = "GatheredOK"
	// GatheredWithError is a reason when data is gathered partially or with another error message
	GatheredWithErrorReason = "GatheredWithError"
)

// GatherJob is the type responsible for controlling a non-periodic Gather execution
type GatherJob struct {
	config.Controller
}

// Gather runs a single gather and stores the generated archive, without uploading it.
// 1. Creates the necessary configs/clients
// 2. Creates the configobserver to get more configs
// 3. Initiates the recorder
// 4. Executes a Gather
// 5. Flushes the results
func (d *GatherJob) Gather(ctx context.Context, kubeConfig, protoKubeConfig *rest.Config) error {
	klog.Infof("Starting insights-operator %s", version.Get().String())
	// these are operator clients
	kubeClient, err := kubernetes.NewForConfig(protoKubeConfig)
	if err != nil {
		return err
	}

	configClient, err := configv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	operatorClient, err := operatorv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	gatherProtoKubeConfig, gatherKubeConfig, metricsGatherKubeConfig, alertsGatherKubeConfig := prepareGatherConfigs(
		protoKubeConfig, kubeConfig, d.Impersonate,
	)

	tpEnabled, err := isTechPreviewEnabled(ctx, configClient)
	if err != nil {
		klog.Error("can't read cluster feature gates: %v", err)
	}
	var gatherConfig v1alpha1.GatherConfig
	if tpEnabled {
		insightsDataGather, err := configClient.ConfigV1alpha1().InsightsDataGathers().Get(ctx, "cluster", metav1.GetOptions{}) //nolint: govet
		if err != nil {
			return err
		}
		gatherConfig = insightsDataGather.Spec.GatherConfig
	}

	// ensure the insight snapshot directory exists
	if _, err = os.Stat(d.StoragePath); err != nil && os.IsNotExist(err) {
		if err = os.MkdirAll(d.StoragePath, 0777); err != nil {
			return fmt.Errorf("can't create --path: %v", err)
		}
	}

	// configobserver synthesizes all config into the status reporter controller
	configObserver := configobserver.New(d.Controller, kubeClient)

	// anonymizer is responsible for anonymizing sensitive data, it can be configured to disable specific anonymization
	anonymizer, err := anonymization.NewAnonymizerFromConfig(
		ctx, gatherKubeConfig, gatherProtoKubeConfig, protoKubeConfig, configObserver, nil)
	if err != nil {
		return err
	}

	// the recorder stores the collected data and we flush at the end.
	recdriver := diskrecorder.New(d.StoragePath)
	rec := recorder.New(recdriver, d.Interval, anonymizer)
	/* 	defer func() {
		if err := rec.Flush(); err != nil {
			klog.Error(err)
		}
	}() */

	authorizer := clusterauthorizer.New(configObserver)
	insightsClient := insightsclient.New(nil, 0, "default", authorizer, gatherKubeConfig)

	gatherers := gather.CreateAllGatherers(
		gatherKubeConfig, gatherProtoKubeConfig, metricsGatherKubeConfig, alertsGatherKubeConfig, anonymizer,
		configObserver, insightsClient,
	)
	uploader := insightsuploader.New(recdriver, insightsClient, configObserver, nil, nil, 0)

	reporter := insightsreport.New(insightsClient, configObserver, operatorClient.InsightsOperators())

	allFunctionReports := make(map[string]gather.GathererFunctionReport)
	gatherTime := metav1.Now()
	for _, gatherer := range gatherers {
		functionReports, err := gather.CollectAndRecordGatherer(ctx, gatherer, rec, &gatherConfig)
		if err != nil {
			klog.Errorf("unable to process gatherer %v, error: %v", gatherer.GetName(), err)
		}

		for i := range functionReports {
			allFunctionReports[functionReports[i].FuncName] = functionReports[i]
		}
	}
	err = gather.RecordArchiveMetadata(mapToArray(allFunctionReports), rec, anonymizer)
	if err != nil {
		klog.Error(err)
	}
	err = rec.Flush()
	if err != nil {
		klog.Error(err)
	}

	err = updateOperatorStatusCR(operatorClient.InsightsOperators(), allFunctionReports, gatherTime)
	if err != nil {
		klog.Error(err)
	}
	lastArchive, err := recdriver.LastArchive()
	if err != nil {
		klog.Error(err)
	}
	err = uploader.Upload(ctx, lastArchive)
	if err != nil {
		klog.Error(err)
	}
	reporter.RetrieveReport()
	return nil
}

// updateOperatorStatusCR gets the 'cluster' insightsoperators.operator.openshift.io resource and updates its status with the last
// gathering details.
func updateOperatorStatusCR(insightsOperatorCLI operatorv1client.InsightsOperatorInterface, allFunctionReports map[string]gather.GathererFunctionReport, gatherTime metav1.Time) error {
	insightsOperatorCR, err := insightsOperatorCLI.Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return err
	}

	updatedOperatorCR := insightsOperatorCR.DeepCopy()
	updatedOperatorCR.Status.GatherStatus = v1.GatherStatus{
		LastGatherTime: gatherTime,
		LastGatherDuration: metav1.Duration{
			Duration: time.Since(gatherTime.Time),
		},
	}

	for k := range allFunctionReports {
		fr := allFunctionReports[k]
		// duration = 0 means the gatherer didn't run
		if fr.Duration == 0 {
			continue
		}

		gs := createGathererStatus(&fr)
		updatedOperatorCR.Status.GatherStatus.Gatherers = append(updatedOperatorCR.Status.GatherStatus.Gatherers, gs)
	}

	_, err = insightsOperatorCLI.UpdateStatus(context.Background(), updatedOperatorCR, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func createGathererStatus(gfr *gather.GathererFunctionReport) v1.GathererStatus {
	gs := v1.GathererStatus{
		Name: gfr.FuncName,
		LastGatherDuration: metav1.Duration{
			// v.Duration is in milliseconds and we need nanoseconds
			Duration: time.Duration(gfr.Duration * 1000000),
		},
	}
	con := metav1.Condition{
		Type:               DataGatheredCondition,
		LastTransitionTime: metav1.Now(),
		Status:             metav1.ConditionFalse,
		Reason:             NoDataGatheredReason,
	}

	if gfr.Panic != nil {
		con.Reason = GatherPanicReason
		con.Message = gfr.Panic.(string)
	}

	if gfr.RecordsCount > 0 {
		con.Status = metav1.ConditionTrue
		con.Reason = GatheredOKReason
		con.Message = fmt.Sprintf("Created %d records in the archive.", gfr.RecordsCount)

		if len(gfr.Errors) > 0 {
			con.Reason = GatheredWithErrorReason
			con.Message = fmt.Sprintf("%s Error: %s", con.Message, strings.Join(gfr.Errors, ","))
		}

		gs.Conditions = append(gs.Conditions, con)
		return gs
	}

	if len(gfr.Errors) > 0 {
		con.Reason = GatherErrorReason
		con.Message = strings.Join(gfr.Errors, ",")
	}

	gs.Conditions = append(gs.Conditions, con)

	return gs
}

func mapToArray(m map[string]gather.GathererFunctionReport) []gather.GathererFunctionReport {
	a := make([]gather.GathererFunctionReport, 0, len(m))
	for _, v := range m {
		a = append(a, v)
	}
	return a
}
