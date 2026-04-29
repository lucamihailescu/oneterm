// Package sshhostkey provides outbound SSH host-key verification for the
// bastion. It replaces the previous use of ssh.InsecureIgnoreHostKey() with a
// trust-on-first-use (TOFU) policy backed by the host_key table.
//
// Modes (set via config.Cfg.Ssh.HostKeyMode):
//
//	"tofu"     — record the fingerprint on first connect, reject mismatches afterward.
//	"strict"   — reject any host whose fingerprint is not already pinned.
//	"insecure" — accept any key. Restores legacy behavior; logs a warning per use.
//
// Default (empty): "tofu".
package sshhostkey

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	gossh "golang.org/x/crypto/ssh"
	"gorm.io/gorm/clause"

	"github.com/veops/oneterm/internal/model"
	"github.com/veops/oneterm/pkg/config"
	dbpkg "github.com/veops/oneterm/pkg/db"
	"github.com/veops/oneterm/pkg/logger"
)

const (
	ModeTOFU     = "tofu"
	ModeStrict   = "strict"
	ModeInsecure = "insecure"
)

// ErrHostKeyMismatch is returned when the presented key does not match the
// stored fingerprint for a (host, port, algo) tuple.
var ErrHostKeyMismatch = errors.New("ssh: host key fingerprint mismatch")

// ErrHostKeyUnknown is returned in strict mode when no fingerprint is on file.
var ErrHostKeyUnknown = errors.New("ssh: host key not pinned (strict mode)")

// mu guards concurrent first-insert races for the same (host, port, algo).
var mu sync.Mutex

// Callback returns a gossh.HostKeyCallback honoring the configured mode.
// Pass it to *gossh.ClientConfig.HostKeyCallback in place of InsecureIgnoreHostKey().
func Callback() gossh.HostKeyCallback {
	mode := config.Cfg.Ssh.HostKeyMode
	if mode == "" {
		mode = ModeTOFU
	}
	return func(hostport string, remote net.Addr, key gossh.PublicKey) error {
		host, port := splitHostPort(hostport)
		fp := fingerprintSHA256(key)
		algo := key.Type()

		switch mode {
		case ModeInsecure:
			logger.L().Warn("ssh host key verification disabled (insecure mode)",
				zap.String("host", host), zap.Int("port", port),
				zap.String("algo", algo), zap.String("fingerprint", fp))
			return nil

		case ModeStrict:
			return verifyAgainstStore(host, port, algo, fp, false)

		default: // tofu
			return verifyAgainstStore(host, port, algo, fp, true)
		}
	}
}

// verifyAgainstStore enforces the policy:
//   - existing row, fingerprint matches    → update last_seen, accept.
//   - existing row, fingerprint differs    → reject (regardless of pinned flag,
//     since unpinned TOFU rows are still authoritative for the lifetime of this
//     row; operators must explicitly clear or re-pin to recover).
//   - no existing row, allowFirstUse=true  → insert (TOFU), accept.
//   - no existing row, allowFirstUse=false → reject (strict mode).
func verifyAgainstStore(host string, port int, algo, fp string, allowFirstUse bool) error {
	mu.Lock()
	defer mu.Unlock()

	var existing model.HostKey
	err := dbpkg.DB.
		Where("host = ? AND port = ? AND algo = ?", host, port, algo).
		First(&existing).Error
	if err == nil {
		if existing.Fingerprint == fp {
			now := time.Now()
			dbpkg.DB.Model(&existing).Update("last_seen", now)
			return nil
		}
		logger.L().Error("ssh host key fingerprint mismatch — possible MITM",
			zap.String("host", host), zap.Int("port", port),
			zap.String("algo", algo),
			zap.String("expected", existing.Fingerprint),
			zap.String("got", fp),
			zap.Bool("pinned", existing.Pinned))
		return fmt.Errorf("%w: host=%s:%d algo=%s expected=%s got=%s",
			ErrHostKeyMismatch, host, port, algo, existing.Fingerprint, fp)
	}

	if !allowFirstUse {
		logger.L().Warn("ssh host key not on file (strict mode); rejecting",
			zap.String("host", host), zap.Int("port", port), zap.String("algo", algo))
		return fmt.Errorf("%w: host=%s:%d algo=%s", ErrHostKeyUnknown, host, port, algo)
	}

	now := time.Now()
	row := model.HostKey{
		Host:        host,
		Port:        port,
		Algo:        algo,
		Fingerprint: fp,
		Pinned:      false,
		FirstSeen:   now,
		LastSeen:    now,
	}
	// OnConflict in case of a race despite the mutex (multi-process deploys).
	if err := dbpkg.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		logger.L().Warn("failed to record host key on TOFU; accepting connection anyway",
			zap.Error(err), zap.String("host", host), zap.Int("port", port))
	} else {
		logger.L().Info("ssh host key recorded (TOFU)",
			zap.String("host", host), zap.Int("port", port),
			zap.String("algo", algo), zap.String("fingerprint", fp))
	}
	return nil
}

func fingerprintSHA256(key gossh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func splitHostPort(hp string) (string, int) {
	host, portStr, err := net.SplitHostPort(hp)
	if err != nil {
		return hp, 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}
