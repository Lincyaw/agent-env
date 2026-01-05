package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *WarmPool) DeepCopyInto(out *WarmPool) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy of this object
func (in *WarmPool) DeepCopy() *WarmPool {
	if in == nil {
		return nil
	}
	out := new(WarmPool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject creates a deep copy as runtime.Object
func (in *WarmPool) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies all properties of WarmPoolList
func (in *WarmPoolList) DeepCopyInto(out *WarmPoolList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]WarmPool, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a deep copy of WarmPoolList
func (in *WarmPoolList) DeepCopy() *WarmPoolList {
	if in == nil {
		return nil
	}
	out := new(WarmPoolList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject creates a deep copy as runtime.Object
func (in *WarmPoolList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies WarmPoolSpec
func (in *WarmPoolSpec) DeepCopyInto(out *WarmPoolSpec) {
	*out = *in
	in.Template.DeepCopyInto(&out.Template)
}

// DeepCopy creates a deep copy of WarmPoolSpec
func (in *WarmPoolSpec) DeepCopy() *WarmPoolSpec {
	if in == nil {
		return nil
	}
	out := new(WarmPoolSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies WarmPoolStatus
func (in *WarmPoolStatus) DeepCopyInto(out *WarmPoolStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopy creates a deep copy of WarmPoolStatus
func (in *WarmPoolStatus) DeepCopy() *WarmPoolStatus {
	if in == nil {
		return nil
	}
	out := new(WarmPoolStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto for Sandbox
func (in *Sandbox) DeepCopyInto(out *Sandbox) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy for Sandbox
func (in *Sandbox) DeepCopy() *Sandbox {
	if in == nil {
		return nil
	}
	out := new(Sandbox)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject for Sandbox
func (in *Sandbox) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto for SandboxList
func (in *SandboxList) DeepCopyInto(out *SandboxList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Sandbox, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy for SandboxList
func (in *SandboxList) DeepCopy() *SandboxList {
	if in == nil {
		return nil
	}
	out := new(SandboxList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject for SandboxList
func (in *SandboxList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto for SandboxSpec
func (in *SandboxSpec) DeepCopyInto(out *SandboxSpec) {
	*out = *in
	in.Resources.DeepCopyInto(&out.Resources)
}

// DeepCopy for SandboxSpec
func (in *SandboxSpec) DeepCopy() *SandboxSpec {
	if in == nil {
		return nil
	}
	out := new(SandboxSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto for SandboxStatus
func (in *SandboxStatus) DeepCopyInto(out *SandboxStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopy for SandboxStatus
func (in *SandboxStatus) DeepCopy() *SandboxStatus {
	if in == nil {
		return nil
	}
	out := new(SandboxStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto for Task
func (in *Task) DeepCopyInto(out *Task) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy for Task
func (in *Task) DeepCopy() *Task {
	if in == nil {
		return nil
	}
	out := new(Task)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject for Task
func (in *Task) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto for TaskList
func (in *TaskList) DeepCopyInto(out *TaskList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Task, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy for TaskList
func (in *TaskList) DeepCopy() *TaskList {
	if in == nil {
		return nil
	}
	out := new(TaskList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject for TaskList
func (in *TaskList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto for TaskSpec
func (in *TaskSpec) DeepCopyInto(out *TaskSpec) {
	*out = *in
	out.Timeout = in.Timeout
	if in.Steps != nil {
		out.Steps = make([]TaskStep, len(in.Steps))
		for i := range in.Steps {
			in.Steps[i].DeepCopyInto(&out.Steps[i])
		}
	}
}

// DeepCopy for TaskSpec
func (in *TaskSpec) DeepCopy() *TaskSpec {
	if in == nil {
		return nil
	}
	out := new(TaskSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto for TaskStep
func (in *TaskStep) DeepCopyInto(out *TaskStep) {
	*out = *in
	if in.Command != nil {
		out.Command = make([]string, len(in.Command))
		copy(out.Command, in.Command)
	}
	if in.Env != nil {
		out.Env = make(map[string]string, len(in.Env))
		for k, v := range in.Env {
			out.Env[k] = v
		}
	}
}

// DeepCopy for TaskStep
func (in *TaskStep) DeepCopy() *TaskStep {
	if in == nil {
		return nil
	}
	out := new(TaskStep)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto for TaskStatus
func (in *TaskStatus) DeepCopyInto(out *TaskStatus) {
	*out = *in
	out.Duration = in.Duration
	if in.StartTime != nil {
		in, out := &in.StartTime, &out.StartTime
		*out = (*in).DeepCopy()
	}
	if in.CompletionTime != nil {
		in, out := &in.CompletionTime, &out.CompletionTime
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopy for TaskStatus
func (in *TaskStatus) DeepCopy() *TaskStatus {
	if in == nil {
		return nil
	}
	out := new(TaskStatus)
	in.DeepCopyInto(out)
	return out
}
