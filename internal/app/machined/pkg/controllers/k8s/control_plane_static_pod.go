// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package k8s

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/AlekSi/pointer"
	"github.com/talos-systems/os-runtime/pkg/controller"
	"github.com/talos-systems/os-runtime/pkg/resource"
	"github.com/talos-systems/os-runtime/pkg/state"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/talos-systems/talos/internal/app/machined/pkg/resources/config"
	"github.com/talos-systems/talos/internal/app/machined/pkg/resources/k8s"
	"github.com/talos-systems/talos/pkg/machinery/constants"
)

// ControlPlaneStaticPodController manages k8s.StaticPod based on control plane configuration.
type ControlPlaneStaticPodController struct{}

// Name implements controller.Controller interface.
func (ctrl *ControlPlaneStaticPodController) Name() string {
	return "k8s.ControlPlaneStaticPodController"
}

// ManagedResources implements controller.Controller interface.
func (ctrl *ControlPlaneStaticPodController) ManagedResources() (resource.Namespace, resource.Type) {
	return k8s.ControlPlaneNamespaceName, k8s.StaticPodType
}

// Run implements controller.Controller interface.
//
//nolint: gocyclo
func (ctrl *ControlPlaneStaticPodController) Run(ctx context.Context, r controller.Runtime, logger *log.Logger) error {
	if err := r.UpdateDependencies([]controller.Dependency{
		{
			Namespace: config.NamespaceName,
			Type:      config.K8sControlPlaneType,
			Kind:      controller.DependencyWeak,
		},
		{
			Namespace: k8s.ControlPlaneNamespaceName,
			Type:      k8s.SecretsStatusType,
			ID:        pointer.ToString(k8s.StaticPodSecretsStaticPodID),
			Kind:      controller.DependencyWeak,
		},
	}); err != nil {
		return fmt.Errorf("error setting up dependencies: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		secretsStatusResource, err := r.Get(ctx, resource.NewMetadata(k8s.ControlPlaneNamespaceName, k8s.SecretsStatusType, k8s.StaticPodSecretsStaticPodID, resource.VersionUndefined))
		if err != nil {
			if state.IsNotFoundError(err) {
				if err = ctrl.teardownAll(ctx, r); err != nil {
					return fmt.Errorf("error tearing down: %w", err)
				}

				continue
			}

			return err
		}

		secretsVersion := secretsStatusResource.(*k8s.SecretsStatus).Status().Version

		for _, pod := range []struct {
			f  func(context.Context, controller.Runtime, *log.Logger, *config.K8sControlPlane, string) error
			id resource.ID
		}{
			{
				f:  ctrl.manageAPIServer,
				id: config.K8sControlPlaneAPIServerID,
			},
			{
				f:  ctrl.manageControllerManager,
				id: config.K8sControlPlaneControllerManagerID,
			},
			{
				f:  ctrl.manageScheduler,
				id: config.K8sControlPlaneSchedulerID,
			},
		} {
			res, err := r.Get(ctx, resource.NewMetadata(config.NamespaceName, config.K8sControlPlaneType, pod.id, resource.VersionUndefined))
			if err != nil {
				if state.IsNotFoundError(err) {
					continue
				}

				return fmt.Errorf("error getting control plane config: %w", err)
			}

			if err = pod.f(ctx, r, logger, res.(*config.K8sControlPlane), secretsVersion); err != nil {
				return fmt.Errorf("error updating static pod for %q: %w", pod.id, err)
			}
		}
	}
}

func (ctrl *ControlPlaneStaticPodController) teardownAll(ctx context.Context, r controller.Runtime) error {
	list, err := r.List(ctx, resource.NewMetadata(k8s.ControlPlaneNamespaceName, k8s.StaticPodType, "", resource.VersionUndefined))
	if err != nil {
		return err
	}

	// TODO: change this to proper teardown sequence

	for _, res := range list.Items {
		if err = r.Destroy(ctx, res.Metadata()); err != nil {
			return err
		}
	}

	return nil
}

func (ctrl *ControlPlaneStaticPodController) manageAPIServer(ctx context.Context, r controller.Runtime, logger *log.Logger, configResource *config.K8sControlPlane, secretsVersion string) error {
	cfg := configResource.APIServer()

	args := []string{
		"/go-runner",
		"/usr/local/bin/kube-apiserver",
		"--enable-admission-plugins=PodSecurityPolicy,NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeClaimResize,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota,Priority,NodeRestriction", //nolint: lll
		"--advertise-address=$(POD_IP)",
		"--allow-privileged=true",
		fmt.Sprintf("--api-audiences=%s", cfg.ControlPlaneEndpoint),
		"--authorization-mode=Node,RBAC",
		"--bind-address=0.0.0.0",
		fmt.Sprintf("--client-ca-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "ca.crt")),
		fmt.Sprintf("--requestheader-client-ca-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "aggregator-ca.crt")),
		"--requestheader-allowed-names=front-proxy-client",
		"--requestheader-extra-headers-prefix=X-Remote-Extra-",
		"--requestheader-group-headers=X-Remote-Group",
		"--requestheader-username-headers=X-Remote-User",
		fmt.Sprintf("--proxy-client-cert-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "front-proxy-client.crt")),
		fmt.Sprintf("--proxy-client-key-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "front-proxy-client.key")),
		fmt.Sprintf("--cloud-provider=%s", cfg.CloudProvider),
		"--enable-bootstrap-token-auth=true",
		"--tls-cipher-suites=TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_RSA_WITH_AES_256_GCM_SHA384,TLS_RSA_WITH_AES_128_GCM_SHA256", //nolint: lll
		fmt.Sprintf("--encryption-provider-config=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "encryptionconfig.yaml")),
		fmt.Sprintf("--audit-policy-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "auditpolicy.yaml")),
		"--audit-log-path=-",
		"--audit-log-maxage=30",
		"--audit-log-maxbackup=3",
		"--audit-log-maxsize=50",
		"--profiling=false",
		fmt.Sprintf("--etcd-cafile=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "etcd-client-ca.crt")),
		fmt.Sprintf("--etcd-certfile=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "etcd-client.crt")),
		fmt.Sprintf("--etcd-keyfile=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "etcd-client.key")),
		fmt.Sprintf("--etcd-servers=%s", strings.Join(cfg.EtcdServers, ",")),
		"--insecure-port=0",
		fmt.Sprintf("--kubelet-client-certificate=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "apiserver-kubelet-client.crt")),
		fmt.Sprintf("--kubelet-client-key=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "apiserver-kubelet-client.key")),
		fmt.Sprintf("--secure-port=%d", cfg.LocalPort),
		fmt.Sprintf("--service-account-issuer=%s", cfg.ControlPlaneEndpoint),
		fmt.Sprintf("--service-account-key-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "service-account.pub")),
		fmt.Sprintf("--service-account-signing-key-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "service-account.key")),
		fmt.Sprintf("--service-cluster-ip-range=%s", cfg.ServiceCIDR),
		fmt.Sprintf("--tls-cert-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "apiserver.crt")),
		fmt.Sprintf("--tls-private-key-file=%s", filepath.Join(constants.KubernetesAPIServerSecretsDir, "apiserver.key")),
		"--kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname",
	}

	for k, v := range cfg.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}

	return r.Update(ctx, k8s.NewStaticPod(k8s.ControlPlaneNamespaceName, "kube-apiserver", nil), func(r resource.Resource) error {
		r.(*k8s.StaticPod).SetPod(&v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-apiserver",
				Namespace: "kube-system",
				Labels: map[string]string{
					"tier":            "control-plane",
					"k8s-app":         "kube-apiserver",
					"secrets-version": secretsVersion,
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "kube-apiserver",
						Image:   cfg.Image,
						Command: args,
						Env: []v1.EnvVar{
							{
								Name: "POD_IP",
								ValueFrom: &v1.EnvVarSource{
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: "status.podIP",
									},
								},
							},
						},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "secrets",
								MountPath: constants.KubernetesAPIServerSecretsDir,
								ReadOnly:  true,
							},
						},
					},
				},
				HostNetwork: true,
				SecurityContext: &v1.PodSecurityContext{
					RunAsNonRoot: pointer.ToBool(true),
					RunAsUser:    pointer.ToInt64(constants.KubernetesRunUser),
				},
				Volumes: []v1.Volume{
					{
						Name: "secrets",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: constants.KubernetesAPIServerSecretsDir,
							},
						},
					},
				},
			},
		})

		return nil
	})
}

func (ctrl *ControlPlaneStaticPodController) manageControllerManager(ctx context.Context, r controller.Runtime,
	logger *log.Logger, configResource *config.K8sControlPlane, secretsVersion string) error {
	cfg := configResource.ControllerManager()

	args := []string{
		"/go-runner",
		"/usr/local/bin/kube-controller-manager",
		"--allocate-node-cidrs=true",
		fmt.Sprintf("--cloud-provider=%s", cfg.CloudProvider),
		fmt.Sprintf("--cluster-cidr=%s", cfg.PodCIDR),
		fmt.Sprintf("--service-cluster-ip-range=%s", cfg.ServiceCIDR),
		fmt.Sprintf("--cluster-signing-cert-file=%s", filepath.Join(constants.KubernetesControllerManagerSecretsDir, "ca.crt")),
		fmt.Sprintf("--cluster-signing-key-file=%s", filepath.Join(constants.KubernetesControllerManagerSecretsDir, "ca.key")),
		"--configure-cloud-routes=false",
		fmt.Sprintf("--kubeconfig=%s", filepath.Join(constants.KubernetesControllerManagerSecretsDir, "kubeconfig")),
		"--leader-elect=true",
		fmt.Sprintf("--root-ca-file=%s", filepath.Join(constants.KubernetesControllerManagerSecretsDir, "ca.crt")),
		fmt.Sprintf("--service-account-private-key-file=%s", filepath.Join(constants.KubernetesControllerManagerSecretsDir, "service-account.key")),
		"--profiling=false",
	}

	for k, v := range cfg.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}

	//nolint: dupl
	return r.Update(ctx, k8s.NewStaticPod(k8s.ControlPlaneNamespaceName, "kube-controller-manager", nil), func(r resource.Resource) error {
		r.(*k8s.StaticPod).SetPod(&v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-controller-manager",
				Namespace: "kube-system",
				Labels: map[string]string{
					"tier":            "control-plane",
					"k8s-app":         "kube-controller-manager",
					"secrets-version": secretsVersion,
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "kube-controller-manager",
						Image:   cfg.Image,
						Command: args,
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "secrets",
								MountPath: constants.KubernetesControllerManagerSecretsDir,
								ReadOnly:  true,
							},
						},
						LivenessProbe: &v1.Probe{
							Handler: v1.Handler{
								HTTPGet: &v1.HTTPGetAction{
									Path: "/healthz",
									Port: intstr.FromInt(10252),
								},
							},
							InitialDelaySeconds: 15,
							TimeoutSeconds:      15,
						},
					},
				},
				HostNetwork: true,
				SecurityContext: &v1.PodSecurityContext{
					RunAsNonRoot: pointer.ToBool(true),
					RunAsUser:    pointer.ToInt64(constants.KubernetesRunUser),
				},
				Volumes: []v1.Volume{
					{
						Name: "secrets",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: constants.KubernetesControllerManagerSecretsDir,
							},
						},
					},
				},
			},
		})

		return nil
	})
}

func (ctrl *ControlPlaneStaticPodController) manageScheduler(ctx context.Context, r controller.Runtime,
	logger *log.Logger, configResource *config.K8sControlPlane, secretsVersion string) error {
	cfg := configResource.Scheduler()

	args := []string{
		"/go-runner",
		"/usr/local/bin/kube-scheduler",
		fmt.Sprintf("--kubeconfig=%s", filepath.Join(constants.KubernetesSchedulerSecretsDir, "kubeconfig")),
		"--leader-elect=true",
		"--profiling=false",
	}

	for k, v := range cfg.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}

	//nolint: dupl
	return r.Update(ctx, k8s.NewStaticPod(k8s.ControlPlaneNamespaceName, "kube-scheduler", nil), func(r resource.Resource) error {
		r.(*k8s.StaticPod).SetPod(&v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-scheduler",
				Namespace: "kube-system",
				Labels: map[string]string{
					"tier":            "control-plane",
					"k8s-app":         "kube-scheduler",
					"secrets-version": secretsVersion,
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "kube-scheduler",
						Image:   cfg.Image,
						Command: args,
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "secrets",
								MountPath: constants.KubernetesSchedulerSecretsDir,
								ReadOnly:  true,
							},
						},
						LivenessProbe: &v1.Probe{
							Handler: v1.Handler{
								HTTPGet: &v1.HTTPGetAction{
									Path: "/healthz",
									Port: intstr.FromInt(10251),
								},
							},
							InitialDelaySeconds: 15,
							TimeoutSeconds:      15,
						},
					},
				},
				HostNetwork: true,
				SecurityContext: &v1.PodSecurityContext{
					RunAsNonRoot: pointer.ToBool(true),
					RunAsUser:    pointer.ToInt64(constants.KubernetesRunUser),
				},
				Volumes: []v1.Volume{
					{
						Name: "secrets",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: constants.KubernetesSchedulerSecretsDir,
							},
						},
					},
				},
			},
		})

		return nil
	})
}