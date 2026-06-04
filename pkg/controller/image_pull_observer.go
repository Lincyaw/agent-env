package controller

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/labels"
)

// ImagePullObserver watches kubelet "Pulled" Events on warm-pool pods and
// records image pull duration and cache hit/miss. Unlike pod container-start
// timing, the kubelet event distinguishes "already present on machine" (cache
// hit) from "Successfully pulled image ... in <dur>" (cache miss), giving an
// accurate cache-hit rate and the true pull cost of large images.
//
// The operator's manager cache is configured (in cmd/operator) with a field
// selector reason=Pulled so only image-pull events are watched.
type ImagePullObserver struct {
	client.Client
	Metrics interfaces.MetricsCollector

	// recorded deduplicates events by "namespace/name". kubelet aggregates
	// repeated identical pulls into a single event whose count is bumped via
	// Update; we record each event once. Entries are pruned when the event is
	// garbage-collected by the apiserver (delivered as a delete → NotFound).
	recorded sync.Map
}

// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch

// pulledDurationRe extracts the pull duration from a kubelet "Pulled" message,
// e.g. `Successfully pulled image "x" in 1m2.345s (1m2.345s including waiting).
// Image size: 12345 bytes.` — capturing the first Go-style duration token.
var pulledDurationRe = regexp.MustCompile(`pulled image .* in ([0-9hmsµun.]+)`)

// Reconcile processes a single kubelet "Pulled" event.
func (o *ImagePullObserver) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if o.Metrics == nil {
		return ctrl.Result{}, nil
	}

	ev := &corev1.Event{}
	if err := o.Get(ctx, req.NamespacedName, ev); err != nil {
		if errors.IsNotFound(err) {
			// Event was GC'd; drop its dedup entry to bound memory.
			o.recorded.Delete(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if ev.Reason != "Pulled" || ev.InvolvedObject.Kind != "Pod" {
		return ctrl.Result{}, nil
	}

	if _, loaded := o.recorded.LoadOrStore(req.NamespacedName.String(), struct{}{}); loaded {
		return ctrl.Result{}, nil
	}

	// Resolve the owning pool via the pod's label. The pod may already be gone
	// (events can outlive pods); in that case we cannot attribute the pull.
	pod := &corev1.Pod{}
	if err := o.Get(ctx, client.ObjectKey{
		Namespace: ev.InvolvedObject.Namespace,
		Name:      ev.InvolvedObject.Name,
	}, pod); err != nil {
		return ctrl.Result{}, nil
	}
	poolName := pod.Labels[labels.PoolLabelKey]
	if poolName == "" {
		return ctrl.Result{}, nil
	}

	result, dur := parsePulledMessage(ev.Message)
	o.Metrics.RecordImagePull(poolName, result, dur)
	return ctrl.Result{}, nil
}

// parsePulledMessage classifies a kubelet "Pulled" event message.
//
//	"Container image \"x\" already present on machine"        -> ("hit", 0)
//	"Successfully pulled image \"x\" in 1m2.3s (...)"          -> ("miss", 1m2.3s)
//
// Unparseable miss messages still count as a miss with zero duration so the
// cache-hit rate stays accurate even if the duration format changes.
func parsePulledMessage(msg string) (string, time.Duration) {
	if strings.Contains(msg, "already present on machine") {
		return "hit", 0
	}
	if m := pulledDurationRe.FindStringSubmatch(msg); len(m) == 2 {
		if d, err := time.ParseDuration(m[1]); err == nil {
			return "miss", d
		}
	}
	return "miss", 0
}

// SetupWithManager registers the observer to watch Pulled events for pods.
func (o *ImagePullObserver) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		ev, ok := obj.(*corev1.Event)
		return ok && ev.Reason == "Pulled" && ev.InvolvedObject.Kind == "Pod"
	})
	return ctrl.NewControllerManagedBy(mgr).
		Named("image-pull-observer").
		For(&corev1.Event{}, builder.WithPredicates(pred)).
		Complete(o)
}

// Name returns the controller name for logging.
func (o *ImagePullObserver) Name() string { return "ImagePullObserver" }
