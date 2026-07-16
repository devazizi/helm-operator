package main

import (
	"os"

	helmVersion "happyhelm.sh/api/helm/v1alpha1"
	reflectorVersion "happyhelm.sh/api/others/v1alpha1"
	controllers "happyhelm.sh/internal/controller"
	helmclient "happyhelm.sh/internal/helm"
	operatorwebhook "happyhelm.sh/internal/webhook"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {

	_ = corev1.AddToScheme(scheme)
	_ = helmVersion.DeployAddToScheme(scheme)
	_ = helmVersion.HelmRepoAddToScheme(scheme)
	_ = reflectorVersion.ReflectorAddToScheme(scheme)
}

func main() {
	ctrl.SetLogger(ctrlzap.New(ctrlzap.UseDevMode(true)))
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           scheme,
		LeaderElection:   true,
		LeaderElectionID: "happyhelm-controller-leader-election",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	helmSDK := helmclient.NewSDKClient()

	if err = (&controllers.HappyHelmReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("DeployChart"),
		Helm:   helmSDK,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DeployChart")
		os.Exit(1)
	}
	if err = (&controllers.HelmRepoReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("Repository"),
		Helm:   helmSDK,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Repository")
		os.Exit(1)
	}

	mgr.GetWebhookServer().Register(operatorwebhook.DeployChartIdentityPath, &admission.Webhook{
		Handler: &operatorwebhook.DeployChartIdentityHandler{},
	})
	if err = (&controllers.ReflectorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("Reflector"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Reflector")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
