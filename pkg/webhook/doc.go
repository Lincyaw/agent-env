package webhook

// Package webhook provides webhook handlers for validating and mutating resources.
//
// To enable webhooks in production:
// 1. Generate webhook certificates
// 2. Update controller manager with webhook server configuration
// 3. Create ValidatingWebhookConfiguration and MutatingWebhookConfiguration resources
// 4. Register webhook handlers in cmd/operator/main.go
//
// For more information on kubebuilder webhooks:
// https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html
