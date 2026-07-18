package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	buildContextDir      = "builds"
	buildContextFilename = "context.tar.gz"
	buildJobPrefix       = "arl-build-"
	buildMaxContextBytes = 512 << 20 // 512 MiB
)

// BuildImage creates a Kaniko Job that builds a container image from the
// provided build context and pushes it to the target registry. The call
// blocks until the Job completes or the context is cancelled.
func (g *Gateway) BuildImage(ctx context.Context, req BuildRequest, contextReader io.Reader) (*BuildResponse, error) {
	if !g.gwConfig.BuildEnabled {
		return nil, fmt.Errorf("image build API is not enabled")
	}
	if req.Image == "" {
		return nil, fmt.Errorf("image is required")
	}
	if g.gwConfig.CheckpointStorePath == "" {
		return nil, fmt.Errorf("checkpoint store path is required for builds (shared PVC)")
	}

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = g.gwConfig.BuildDefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	buildID := randomSuffix(8)
	ns := g.runtimeNamespace()

	// Persist the build context tarball to the shared PVC.
	contextDir := filepath.Join(g.gwConfig.CheckpointStorePath, buildContextDir, buildID)
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return nil, fmt.Errorf("create build context dir: %w", err)
	}
	contextPath := filepath.Join(contextDir, buildContextFilename)
	if err := writeContextFile(contextPath, contextReader); err != nil {
		os.RemoveAll(contextDir)
		return nil, fmt.Errorf("write build context: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(contextDir); err != nil {
			log.Printf("Warning: failed to clean up build context %s: %v", contextDir, err)
		}
	}()

	// Create the Kaniko Job.
	job := g.buildKanikoJob(buildID, ns, req)
	if err := g.k8sClient.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("create build job: %w", err)
	}

	// Poll until completion.
	resp, err := g.waitForBuildJob(ctx, job.Name, ns, buildID)
	if err != nil {
		return nil, err
	}

	// Best-effort cleanup of the completed Job.
	_ = g.k8sClient.Delete(context.Background(), job, ctrlclient.PropagationPolicy(metav1.DeletePropagationBackground))

	return resp, nil
}

func writeContextFile(path string, r io.Reader) (retErr error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			f.Close()
		}
	}()

	lr := &io.LimitedReader{R: r, N: buildMaxContextBytes + 1}
	n, err := io.Copy(f, lr)
	if err != nil {
		return err
	}
	if n > buildMaxContextBytes {
		return fmt.Errorf("build context exceeds maximum size of %d bytes", buildMaxContextBytes)
	}
	return f.Close()
}

func (g *Gateway) buildKanikoJob(buildID, ns string, req BuildRequest) *batchv1.Job {
	args := []string{
		"--context=tar:///workspace/" + buildContextFilename,
		"--destination=" + req.Image,
	}
	if req.Cache {
		args = append(args, "--cache=true")
	}
	if req.Dockerfile != "" {
		args = append(args, "--dockerfile="+req.Dockerfile)
	}
	for k, v := range req.BuildArgs {
		args = append(args, fmt.Sprintf("--build-arg=%s=%s", k, v))
	}

	var backoffLimit int32
	ttl := int32(300)

	volumes := []corev1.Volume{
		{
			Name: "build-context",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: g.gwConfig.BuildCheckpointPVC,
				},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "build-context",
			MountPath: "/workspace",
			SubPath:   filepath.Join(buildContextDir, buildID),
			ReadOnly:  true,
		},
	}

	if g.gwConfig.BuildRegistrySecret != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "docker-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: g.gwConfig.BuildRegistrySecret,
					Items: []corev1.KeyToPath{
						{Key: ".dockerconfigjson", Path: "config.json"},
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "docker-config",
			MountPath: "/kaniko/.docker",
			ReadOnly:  true,
		})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildJobPrefix + buildID,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "arl-gateway",
				"app.kubernetes.io/component":  "image-build",
				"arl/build-id":                 buildID,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:         "kaniko",
							Image:        g.gwConfig.BuildKanikoImage,
							Args:         args,
							VolumeMounts: volumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}
}

func (g *Gateway) waitForBuildJob(ctx context.Context, jobName, ns, buildID string) (*BuildResponse, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &BuildResponse{
				Status: "failed",
				Log:    "build timed out",
			}, ctx.Err()
		case <-ticker.C:
			var job batchv1.Job
			if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, &job); err != nil {
				return nil, fmt.Errorf("get build job: %w", err)
			}

			for _, cond := range job.Status.Conditions {
				if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
					buildLog := g.readBuildLogs(ctx, jobName, ns)
					digest := parseBuildDigest(buildLog)
					image := parseDestinationArg(job.Spec.Template.Spec.Containers[0].Args)
					return &BuildResponse{
						Image:  image,
						Digest: digest,
						Status: "success",
						Log:    buildLog,
					}, nil
				}
				if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
					buildLog := g.readBuildLogs(ctx, jobName, ns)
					return &BuildResponse{
						Status: "failed",
						Log:    buildLog,
					}, nil
				}
			}
		}
	}
}

// readBuildLogs fetches logs from the first pod owned by the build Job.
func (g *Gateway) readBuildLogs(ctx context.Context, jobName, ns string) string {
	var podList corev1.PodList
	if err := g.k8sClient.List(ctx, &podList,
		ctrlclient.InNamespace(ns),
		ctrlclient.MatchingLabels{"job-name": jobName},
	); err != nil || len(podList.Items) == 0 {
		return ""
	}

	podName := podList.Items[0].Name
	return g.readPodLogs(ctx, podName, ns, "kaniko")
}

// readPodLogs reads logs from a specific container in a pod via the REST client.
func (g *Gateway) readPodLogs(ctx context.Context, podName, ns, container string) string {
	if g.k8sClientset == nil {
		return ""
	}

	tailLines := int64(200)
	req := g.k8sClientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return ""
	}
	defer stream.Close()

	data, err := io.ReadAll(io.LimitReader(stream, 64<<10))
	if err != nil {
		return ""
	}
	return string(data)
}

func parseDestinationArg(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--destination=") {
			return strings.TrimPrefix(a, "--destination=")
		}
	}
	return ""
}

// parseBuildDigest extracts the image digest from Kaniko log output.
// Kaniko prints a line like: "<image>: digest: sha256:<hex> size: <n>"
func parseBuildDigest(logOutput string) string {
	for _, line := range strings.Split(logOutput, "\n") {
		if idx := strings.Index(line, "digest: "); idx >= 0 {
			rest := line[idx+len("digest: "):]
			if spIdx := strings.Index(rest, " "); spIdx > 0 {
				return rest[:spIdx]
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// handleBuild returns an HTTP handler for POST /v1/build.
// The endpoint accepts a multipart form with:
//   - image (string) - target image reference
//   - context (file) - build context tar.gz
//   - dockerfile (string, optional) - Dockerfile path within context
//   - build_args (string, optional) - JSON-encoded map[string]string
//   - timeout (string, optional) - timeout in seconds
//   - cache (string, optional) - "true" to enable layer caching
func handleBuild(gw *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
			return
		}
		defer r.MultipartForm.RemoveAll()

		image := r.FormValue("image")
		if image == "" {
			writeError(w, http.StatusBadRequest, "image field is required")
			return
		}

		contextFile, _, err := r.FormFile("context")
		if err != nil {
			writeError(w, http.StatusBadRequest, "context file is required: "+err.Error())
			return
		}
		defer contextFile.Close()

		req := BuildRequest{
			Image:      image,
			Dockerfile: r.FormValue("dockerfile"),
			Cache:      r.FormValue("cache") == "true",
		}

		if v := r.FormValue("timeout"); v != "" {
			var secs int
			if _, err := fmt.Sscanf(v, "%d", &secs); err == nil {
				req.TimeoutSeconds = secs
			}
		}

		if v := r.FormValue("build_args"); v != "" {
			var buildArgs map[string]string
			if err := json.Unmarshal([]byte(v), &buildArgs); err != nil {
				writeError(w, http.StatusBadRequest, "invalid build_args JSON: "+err.Error())
				return
			}
			req.BuildArgs = buildArgs
		}

		resp, err := gw.BuildImage(r.Context(), req, contextFile)
		if err != nil {
			if resp != nil {
				// Timeout with partial response.
				writeJSON(w, http.StatusOK, resp)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		status := http.StatusOK
		if resp.Status == "failed" {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, resp)
	}
}
