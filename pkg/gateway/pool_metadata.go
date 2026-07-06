package gateway

import (
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

func applyPoolProfileMetadata(meta *metav1.ObjectMeta, profile string) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return
	}
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[labels.PoolProfileAnnotation] = profile
	setLabelIfValid(meta, labels.PoolProfileLabelKey, profile)
}

func applyManagedPoolMetadata(meta *metav1.ObjectMeta, managed bool) {
	if !managed {
		return
	}
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[labels.ManagedPoolAnnotation] = "true"
	setLabelIfValid(meta, labels.ManagedPoolLabelKey, "true")
}

func applyPoolStateMetadata(meta *metav1.ObjectMeta, state string) {
	state = strings.TrimSpace(state)
	if state == "" {
		return
	}
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[labels.PoolStateAnnotation] = state
	setLabelIfValid(meta, labels.PoolStateLabelKey, state)
}

func applyPoolLastUsedMetadata(meta *metav1.ObjectMeta, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[labels.PoolLastUsedAnnotation] = at.UTC().Format(time.RFC3339)
}

func setLabelIfValid(meta *metav1.ObjectMeta, key, value string) {
	if !validLabelValue.MatchString(value) {
		if meta.Labels != nil {
			delete(meta.Labels, key)
		}
		return
	}
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels[key] = value
}

func ensureObjectAnnotations(meta *metav1.ObjectMeta) map[string]string {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	return meta.Annotations
}
