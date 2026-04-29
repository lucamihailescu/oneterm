package model

import (
	"testing"
	"time"
)

func TestAccessRequest_IsActive(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)

	tests := []struct {
		name string
		ar   AccessRequest
		want bool
	}{
		{
			name: "approved within window",
			ar: AccessRequest{
				Status: AccessRequestApproved, ValidFrom: &earlier, ExpiresAt: &later,
			},
			want: true,
		},
		{
			name: "approved before valid_from",
			ar: AccessRequest{
				Status: AccessRequestApproved, ValidFrom: &later, ExpiresAt: &later,
			},
			want: false,
		},
		{
			name: "approved after expires_at",
			ar: AccessRequest{
				Status: AccessRequestApproved, ValidFrom: &earlier, ExpiresAt: &earlier,
			},
			want: false,
		},
		{
			name: "approved exactly at expires_at is no longer active",
			ar: AccessRequest{
				Status: AccessRequestApproved, ValidFrom: &earlier, ExpiresAt: &now,
			},
			want: false,
		},
		{
			name: "pending never active",
			ar: AccessRequest{
				Status: AccessRequestPending, ValidFrom: &earlier, ExpiresAt: &later,
			},
			want: false,
		},
		{
			name: "rejected never active",
			ar: AccessRequest{
				Status: AccessRequestRejected, ValidFrom: &earlier, ExpiresAt: &later,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ar.IsActive(now); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}
