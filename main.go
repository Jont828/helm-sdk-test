package main

import (
	"context"
	"flag"
	"strings"

	"github.com/pkg/errors"
	"helm.sh/helm/pkg/getter"
	"helm.sh/helm/pkg/storage/driver"
	helmCli "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/klog/v2"

	helmAction "helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmLoader "helm.sh/helm/v3/pkg/chart/loader"
	helmVals "helm.sh/helm/v3/pkg/cli/values"
	helmGetter "helm.sh/helm/v3/pkg/getter"
)

func main() {
	var kubeconfigPath string
	flag.StringVar(&kubeconfigPath, "kubeconfig", "", "path to the kubeconfig file")

	var chartName string
	flag.StringVar(&chartName, "chartName", "nginx-ingress", "name of the helm chart")

	var releaseName string
	flag.StringVar(&releaseName, "releaseName", "nginx-ingress", "name of the helm release")

	var repoURL string
	flag.StringVar(&repoURL, "repoURL", "https://helm.nginx.com/stable", "url of the helm chart repository")

	var version string
	flag.StringVar(&version, "version", "", "version of the helm chart (can be empty)")

	var commaValues string
	flag.StringVar(&commaValues, "values", "", "comma separated list of values, i.e. key1=value1,key2=value2")

	flag.Parse()

	values := strings.Split(commaValues, ",")
	for i, v := range values {
		values[i] = strings.TrimSpace(v)
	}

	klog.Infoln("Received CLI Args:")
	klog.Infoln("kubeconfigPath:", kubeconfigPath)
	klog.Infoln("chartName:", chartName)
	klog.Infoln("releaseName:", releaseName)
	klog.Infoln("repoURL:", repoURL)
	klog.Infoln("version:", version)
	klog.Infoln("values:", values)
	klog.Infoln("tail:", flag.Args())

	release, err := InstallOrUpdate(kubeconfigPath, chartName, releaseName, repoURL, version, values)

	if err != nil {
		klog.Error("Failed to install or update helm release: ", err)
	} else {
		klog.Infof("Successfully installed or updated release %s at revision %d", release.Name, release.Version)
	}
}

func InstallOrUpdate(kubeconfigPath string, chartName string, releaseName string, repoURL string, version string, values []string) (*release.Release, error) {
	ctx := context.TODO()
	klog.Info("Initializing settings")
	settings := helmCli.New()
	settings.KubeConfig = kubeconfigPath

	actionConfig := new(helmAction.Configuration)
	klog.Info("Initializing action config")
	if err := actionConfig.Init(settings.RESTClientGetter(), "default", "secret", klog.Infof); err != nil {
		return nil, err
	}

	historyClient := helmAction.NewHistory(actionConfig)
	historyClient.Max = 1
	if _, err := historyClient.Run(releaseName); err == driver.ErrReleaseNotFound {
		klog.Info("Release not found, installing it now")
		installClient := helmAction.NewInstall(actionConfig)

		klog.Info("Locating chart")
		cp, err := installClient.ChartPathOptions.LocateChart(chartName, settings)
		if err != nil {
			return nil, err
		}
		klog.Info("Located chart at path", cp)

		p := getter.All(settings)
		valueOpts := &helmVals.Options{
			Values: values,
		}
		vals, err := valueOpts.MergeValues(p)
		if err != nil {
			return nil, err
		}

		// Check chart dependencies to make sure all are present in /charts
		chartRequested, err := loader.Load(cp)
		if err != nil {
			return nil, err
		}

		release, err := installClient.RunWithContext(ctx, chartRequested, vals)
		if err != nil {
			return nil, err
		}

		return release, nil
	}

	upgradeClient := helmAction.NewUpgrade(actionConfig)
	upgradeClient.Install = true
	upgradeClient.RepoURL = repoURL
	upgradeClient.Version = version
	upgradeClient.Namespace = "default"
	klog.Info("Locating chart")
	cp, err := upgradeClient.ChartPathOptions.LocateChart(chartName, settings)
	if err != nil {
		return nil, err
	}
	klog.Info("Located chart at path", cp)

	p := helmGetter.All(settings)
	valueOpts := &helmVals.Options{
		Values: values,
	}
	vals, err := valueOpts.MergeValues(p)
	if err != nil {
		return nil, err
	}
	chartRequested, err := helmLoader.Load(cp)
	if err != nil {
		return nil, err
	}
	if chartRequested == nil {
		return nil, errors.Errorf("failed to load request chart %s", chartName)
	}

	release, err := upgradeClient.RunWithContext(ctx, releaseName, chartRequested, vals)
	if err != nil {
		return nil, err
	}

	return release, nil
}
