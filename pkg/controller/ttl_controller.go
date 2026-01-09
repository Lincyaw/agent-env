// Copyright 2024 ARL-Infra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"encoding/json"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/config"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/middleware"
)

const (
	// auditTimeFormat is the standard time format used for audit records
	auditTimeFormat = time.RFC3339
)

// TTLReconciler reconciles completed Tasks for TTL-based cleanup
type TTLReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Config      *config.Config
	AuditWriter interfaces.AuditWriter
	Metrics     interfaces.MetricsCollector
	Middleware  *middleware.Chain
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=tasks,verbs=get;list;watch;delete

// Reconcile handles TTL-based cleanup of completed Tasks
func (r *TTLReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.Middleware != nil {
		if err := r.Middleware.ExecuteBefore(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
		defer r.Middleware.ExecuteAfter(ctx, req, nil)
	}

	return r.reconcile(ctx, req)
}

func (r *TTLReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	task := &arlv1alpha1.Task{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Only process completed tasks
	if task.Status.State != arlv1alpha1.TaskStateSucceeded &&
		task.Status.State != arlv1alpha1.TaskStateFailed {
		return ctrl.Result{}, nil
	}

	// Check if TTL is set
	if task.Spec.TTLSecondsAfterFinished == nil {
		return ctrl.Result{}, nil
	}

	// Check if completion time is set
	if task.Status.CompletionTime == nil {
		return ctrl.Result{}, nil
	}

	ttl := time.Duration(*task.Spec.TTLSecondsAfterFinished) * time.Second
	expiration := task.Status.CompletionTime.Add(ttl)

	if time.Now().Before(expiration) {
		// Not yet expired, requeue for later
		remaining := time.Until(expiration)
		logger.Info("Task not yet expired, requeueing",
			"task", task.Name,
			"remaining", remaining)
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// Task has expired, write audit log and delete
	logger.Info("Task TTL expired, cleaning up",
		"task", task.Name,
		"ttl", ttl)

	// Write audit record before deletion
	if r.AuditWriter != nil {
		// Serialize steps (input) to JSON
		inputJSON, err := json.Marshal(task.Spec.Steps)
		if err != nil {
			logger.Error(err, "Failed to marshal task steps to JSON for audit")
		}

		record := interfaces.TaskAuditRecord{
			TraceID:    task.Spec.TraceID,
			Namespace:  task.Namespace,
			Name:       task.Name,
			SandboxRef: task.Spec.SandboxRef,
			State:      string(task.Status.State),
			ExitCode:   task.Status.ExitCode,
			Duration:   task.Status.Duration.Duration.String(),
			StepCount:  len(task.Spec.Steps),
			Input:      string(inputJSON),
			Stdout:     task.Status.Stdout,
			Stderr:     task.Status.Stderr,
		}
		if task.Status.StartTime != nil {
			record.StartTime = task.Status.StartTime.Time
		}
		if task.Status.CompletionTime != nil {
			record.CompletionTime = task.Status.CompletionTime.Time
		}
		if err := r.AuditWriter.WriteTaskCompletion(ctx, record); err != nil {
			logger.Error(err, "Failed to write task audit record")
			if r.Metrics != nil {
				r.Metrics.RecordAuditWriteError("task")
			}
		}
	}

	// Delete the task
	if err := r.Delete(ctx, task); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// Record cleanup metric
	if r.Metrics != nil {
		r.Metrics.RecordTaskCleanup(task.Namespace, string(task.Status.State))
	}

	logger.Info("Task cleaned up successfully", "task", task.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *TTLReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.Task{}).
		Named("ttl").
		Complete(r)
}

// Name returns the controller name for logging
func (r *TTLReconciler) Name() string {
	return "TTL"
}
