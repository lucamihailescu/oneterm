package model

import "time"

// HostKey records the SSH host key fingerprint observed for a given
// (host, port) endpoint. Together with the trust-on-first-use logic in
// internal/sshhostkey, it prevents man-in-the-middle attacks on outbound
// SSH/SFTP connections from the bastion to managed assets and gateways.
type HostKey struct {
	Id          int    `json:"id" gorm:"column:id;primarykey;autoIncrement"`
	Host        string `json:"host" gorm:"column:host;size:255;uniqueIndex:host_port_algo"`
	Port        int    `json:"port" gorm:"column:port;uniqueIndex:host_port_algo"`
	Algo        string `json:"algo" gorm:"column:algo;size:64;uniqueIndex:host_port_algo"`
	Fingerprint string `json:"fingerprint" gorm:"column:fingerprint;size:128;not null"`
	// Pinned=true means an operator has reviewed and locked this fingerprint;
	// any subsequent change will be rejected. Pinned=false means the row was
	// captured by trust-on-first-use and may be re-pinned without intervention.
	Pinned    bool      `json:"pinned" gorm:"column:pinned;default:false"`
	FirstSeen time.Time `json:"first_seen" gorm:"column:first_seen"`
	LastSeen  time.Time `json:"last_seen" gorm:"column:last_seen"`
}

func (HostKey) TableName() string { return "host_key" }

var DefaultHostKey = &HostKey{}
