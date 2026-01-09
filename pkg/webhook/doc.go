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

package webhook

// Package webhook provides webhook handlers for validating and mutating resources.
//
// To enable webhooks in production:
// 1. Generate webhook certificates
// 2. Update controller manager with webhook server configuration
// 3. Create ValidatingWebhookConfiguration and MutatingWebhookConfiguration resources
// 4. Register webhook handlers in cmd/operator/main.go
//
// Example webhook setup in main.go:
//
//   if cfg.EnableWebhooks {
//       if err := mgr.AddWebhook("task-validator", &webhook.TaskValidator{}); err != nil {
//           setupLog.Error(err, "unable to create webhook", "webhook", "TaskValidator")
//           os.Exit(1)
//       }
//   }
//
// For more information on kubebuilder webhooks:
// https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html
