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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GameSystem string
type SourceType string
type SaveRetention string
type GamePhase string

const (
	SystemPS1  GameSystem = "ps1"
	SystemPS2  GameSystem = "ps2"
	SystemPSP  GameSystem = "psp"
	SystemSNES GameSystem = "snes"
	SystemN64  GameSystem = "n64"
	SystemGBA  GameSystem = "gba"
	SystemDS   GameSystem = "ds"
	SystemGC   GameSystem = "gc"
	SystemWii  GameSystem = "wii"

	SourceGDrive  SourceType = "gdrive"
	SourceHTTP    SourceType = "http"
	SourceArchive SourceType = "archive"

	RetentionPermanent   SaveRetention = "permanent"
	RetentionGracePeriod SaveRetention = "grace-period"
	RetentionDelete      SaveRetention = "delete"

	PhasePending     GamePhase = "Pending"
	PhaseDownloading GamePhase = "Downloading"
	PhaseReady       GamePhase = "Ready"
	PhaseFailed      GamePhase = "Failed"
)

type GameSource struct {
	Type SourceType `json:"type"`
	URL  string     `json:"url"`
}

// GameSpec defines the desired state of Game
type GameSpec struct {
	Title          string        `json:"title"`
	System         GameSystem    `json:"system"`
	Source         GameSource    `json:"source"`
	Filename       string        `json:"filename"`
	ChecksumSha256 string        `json:"checksumSha256,omitempty"`
	SaveRetention  SaveRetention `json:"saveRetention,omitempty"`
	RomEnabled     bool          `json:"romEnabled,omitempty"`
	ConfirmDelete  bool          `json:"confirmDelete,omitempty"`
}

// GameStatus defines the observed state of Game.
type GameStatus struct {
	Phase        GamePhase    `json:"phase,omitempty"`
	Progress     int          `json:"progress,omitempty"`
	RomsPath     string       `json:"romsPath,omitempty"`
	DownloadedAt *metav1.Time `json:"downloadedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Title",type=string,JSONPath=`.spec.title`
// +kubebuilder:printcolumn:name="System",type=string,JSONPath=`.spec.system`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Progress",type=integer,JSONPath=`.status.progress`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Game is the Schema for the games API
type Game struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GameSpec   `json:"spec,omitempty"`
	// +optional
	Status GameStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GameList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Game `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Game{}, &GameList{})
}
