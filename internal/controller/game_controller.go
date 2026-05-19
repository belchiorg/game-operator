/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gamingv1alpha1 "github.com/belchiorg/game-operator/api/v1alpha1"
)

const (
	finalizer       = "gaming.home/finalizer"
	romsBasePath    = "/mnt/data/roms"
	downloaderImage = "ghcr.io/belchiorg/game-downloader:latest"
	jobTTL          = int32(300)
)

// GameReconciler reconciles a Game object
type GameReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gaming.home,resources=games,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gaming.home,resources=games/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gaming.home,resources=games/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Game object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *GameReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	game := &gamingv1alpha1.Game{}
	if err := r.Get(ctx, req.NamespacedName, game); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !game.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, game)
	}

	if !controllerutil.ContainsFinalizer(game, finalizer) {
		controllerutil.AddFinalizer(game, finalizer)
		if err := r.Update(ctx, game); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if game.Status.Phase == gamingv1alpha1.PhaseReady {
		return ctrl.Result{}, nil
	}

	romPath := filepath.Join(romsBasePath, string(game.Spec.System), game.Spec.Filename)

	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobNameFor(game), Namespace: game.Namespace}, job)
	if errors.IsNotFound(err) {
		if err := r.setPhase(ctx, game, gamingv1alpha1.PhasePending); err != nil {
			return ctrl.Result{}, err
		}
		newJob := r.buildDownloadJob(game, romPath)
		if err := r.Create(ctx, newJob); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("created download job", "job", newJob.Name)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	return r.syncFromJob(ctx, game, job, romPath)
}

func (r *GameReconciler) syncFromJob(ctx context.Context, game *gamingv1alpha1.Game, job *batchv1.Job, romPath string) (ctrl.Result, error) {
	if job.Status.Active > 0 {
		patch := game.DeepCopy()
		patch.Status.Phase = gamingv1alpha1.PhaseDownloading
		if err := r.Status().Patch(ctx, patch, client.MergeFrom(game)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			now := metav1.Now()
			patch := game.DeepCopy()
			patch.Status.Phase = gamingv1alpha1.PhaseReady
			patch.Status.Progress = 100
			patch.Status.RomsPath = romPath
			patch.Status.DownloadedAt = &now
			return ctrl.Result{}, r.Status().Patch(ctx, patch, client.MergeFrom(game))
		}
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return ctrl.Result{}, r.setPhase(ctx, game, gamingv1alpha1.PhaseFailed)
		}
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *GameReconciler) handleDeletion(ctx context.Context, game *gamingv1alpha1.Game) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(game, finalizer) {
		return ctrl.Result{}, nil
	}

	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobNameFor(game), Namespace: game.Namespace}, job); err == nil {
		_ = r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
	}

	cleanJob := r.buildCleanupJob(game)
	_ = r.Create(ctx, cleanJob)

	controllerutil.RemoveFinalizer(game, finalizer)
	return ctrl.Result{}, r.Update(ctx, game)
}

func (r *GameReconciler) buildDownloadJob(game *gamingv1alpha1.Game, romPath string) *batchv1.Job {
	ttl := jobTTL
	backoffLimit := int32(2)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobNameFor(game),
			Namespace: game.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(game, gamingv1alpha1.GroupVersion.WithKind("Game")),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "downloader",
						Image: downloaderImage,
						Env: []corev1.EnvVar{
							{Name: "SOURCE_TYPE", Value: string(game.Spec.Source.Type)},
							{Name: "SOURCE_URL", Value: game.Spec.Source.URL},
							{Name: "DEST_PATH", Value: romPath},
							{Name: "CHECKSUM", Value: game.Spec.ChecksumSha256},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "roms", MountPath: "/mnt/data/roms"},
							{Name: "rclone-config", MountPath: "/etc/rclone"},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "roms",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/data/roms"},
							},
						},
						{
							Name: "rclone-config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "rclone-config",
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *GameReconciler) buildCleanupJob(game *gamingv1alpha1.Game) *batchv1.Job {
	ttl := jobTTL
	romPath := filepath.Join(romsBasePath, string(game.Spec.System), game.Spec.Filename)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cleanup", game.Name),
			Namespace: game.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    "cleanup",
						Image:   "busybox",
						Command: []string{"rm", "-f", romPath},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "roms", MountPath: "/mnt/data/roms"},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "roms",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/data/roms"},
						},
					}},
				},
			},
		},
	}
}

func (r *GameReconciler) setPhase(ctx context.Context, game *gamingv1alpha1.Game, phase gamingv1alpha1.GamePhase) error {
	patch := game.DeepCopy()
	patch.Status.Phase = phase
	return r.Status().Patch(ctx, patch, client.MergeFrom(game))
}

func jobNameFor(game *gamingv1alpha1.Game) string {
	return fmt.Sprintf("%s-download", game.Name)
}

func (r *GameReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gamingv1alpha1.Game{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
