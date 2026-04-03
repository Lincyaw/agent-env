package configenv

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
)

func TestRenderSpecRendersTemplatedValuesAcrossFields(t *testing.T) {
	cfg := &arlv1alpha1.ConfigEnvSpec{
		Vars: map[string]string{
			"name":  "demo",
			"token": "super-secret",
			"file":  "config.yaml",
		},
		EnvVars: []corev1.EnvVar{{
			Name:  "APP_{{ .Vars.name }}",
			Value: "{{ .Vars.token }}",
		}},
		ConfigMaps: []arlv1alpha1.ConfigMapTemplate{{
			ConfigMap: corev1.ConfigMap{
				Data: map[string]string{
					"app.conf": "name={{ .Vars.name }}",
				},
			},
			Inject: &arlv1alpha1.VolumeInjection{
				Container: "{{ .Vars.name }}",
				MountPath: "/etc/{{ .Vars.name }}",
				SubPath:   "{{ .Vars.file }}",
			},
		}},
		Secrets: []arlv1alpha1.SecretTemplate{{
			Secret: corev1.Secret{
				StringData: map[string]string{
					"token": "{{ .Vars.token }}",
				},
			},
			Inject: &arlv1alpha1.SecretInjection{
				Volume: &arlv1alpha1.VolumeInjection{
					Container: "{{ .Vars.name }}",
					MountPath: "/var/run/{{ .Vars.name }}",
					SubPath:   "{{ .Vars.file }}",
				},
				AsEnv: []arlv1alpha1.SecretEnvVar{{
					Name: "SECRET_{{ .Vars.name }}",
					Key:  "{{ .Vars.file }}",
				}},
			},
		}},
	}

	rendered, err := RenderSpec(cfg)
	if err != nil {
		t.Fatalf("RenderSpec returned error: %v", err)
	}

	if got := rendered.EnvVars[0].Name; got != "APP_demo" {
		t.Fatalf("env var name = %q, want %q", got, "APP_demo")
	}
	if got := rendered.EnvVars[0].Value; got != "super-secret" {
		t.Fatalf("env var value = %q, want %q", got, "super-secret")
	}
	if got := rendered.ConfigMaps[0].Data["app.conf"]; got != "name=demo" {
		t.Fatalf("configMap data = %q, want %q", got, "name=demo")
	}
	if got := rendered.ConfigMaps[0].Inject.SubPath; got != "config.yaml" {
		t.Fatalf("configMap subPath = %q, want %q", got, "config.yaml")
	}
	if got := rendered.Secrets[0].StringData["token"]; got != "super-secret" {
		t.Fatalf("secret stringData = %q, want %q", got, "super-secret")
	}
	if got := rendered.Secrets[0].Inject.Volume.SubPath; got != "config.yaml" {
		t.Fatalf("secret subPath = %q, want %q", got, "config.yaml")
	}
	if got := rendered.Secrets[0].Inject.AsEnv[0].Name; got != "SECRET_demo" {
		t.Fatalf("secret env name = %q, want %q", got, "SECRET_demo")
	}
	if got := rendered.Secrets[0].Inject.AsEnv[0].Key; got != "config.yaml" {
		t.Fatalf("secret env key = %q, want %q", got, "config.yaml")
	}
}

func TestRenderSpecRejectsUnknownTemplateInEnvValue(t *testing.T) {
	cfg := &arlv1alpha1.ConfigEnvSpec{
		Vars: map[string]string{"name": "demo"},
		EnvVars: []corev1.EnvVar{{
			Name:  "APP_NAME",
			Value: "{{ .Vars.missing }}",
		}},
	}

	if _, err := RenderSpec(cfg); err == nil {
		t.Fatal("RenderSpec succeeded for env value with unknown template key")
	}
}
