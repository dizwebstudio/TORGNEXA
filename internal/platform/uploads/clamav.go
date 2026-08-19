package uploads

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const clamAVChunkBytes = 32 * 1024

// ClamAVConfig is operator-owned configuration. Address is never derived from
// upload metadata, so this adapter does not create user-controlled network egress.
type ClamAVConfig struct {
	Network          string
	Address          string
	EngineVersion    string
	SignatureVersion string
	Timeout          time.Duration
	MaxBytes         int64
}

func (c ClamAVConfig) Validate() error {
	if (c.Network != "tcp" && c.Network != "unix") || strings.TrimSpace(c.Address) == "" || strings.ContainsAny(c.Address, "\r\n\x00") || !validScannerText(c.EngineVersion) || !validScannerText(c.SignatureVersion) || c.Timeout <= 0 || c.Timeout > 2*time.Minute || c.MaxBytes <= 0 || c.MaxBytes > 10*1024*1024*1024 {
		return ErrInvalid
	}
	return nil
}

type clamAVDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type ClamAVScanner struct {
	config ClamAVConfig
	dialer clamAVDialer
}

func NewClamAVScanner(config ClamAVConfig) (*ClamAVScanner, error) {
	if config.Validate() != nil {
		return nil, ErrInvalid
	}
	return &ClamAVScanner{config: config, dialer: &net.Dialer{Timeout: config.Timeout}}, nil
}

func newClamAVScanner(config ClamAVConfig, dialer clamAVDialer) (*ClamAVScanner, error) {
	if config.Validate() != nil || dialer == nil {
		return nil, ErrInvalid
	}
	return &ClamAVScanner{config: config, dialer: dialer}, nil
}

func (s *ClamAVScanner) Scan(ctx context.Context, request ScanRequest, source io.Reader) (ScanResult, error) {
	if ctx == nil || s == nil || s.dialer == nil || s.config.Validate() != nil || source == nil || !request.UploadID.Valid() || !sha256Pattern.MatchString(request.SHA256) || request.SizeBytes < 0 || request.SizeBytes > s.config.MaxBytes || !validMediaType(request.MediaType) || !sourcePattern.MatchString(request.Policy) {
		return ScanResult{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	conn, err := s.dialer.DialContext(ctx, s.config.Network, s.config.Address)
	if err != nil {
		return ScanResult{ScannerName: "clamav", EngineVersion: s.config.EngineVersion, SignatureVersion: s.config.SignatureVersion, Status: ScannerError}, ErrScannerUnavailable
	}
	defer conn.Close()
	deadline := time.Now().Add(s.config.Timeout)
	_ = conn.SetDeadline(deadline)
	if _, err := io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return ScanResult{ScannerName: "clamav", EngineVersion: s.config.EngineVersion, SignatureVersion: s.config.SignatureVersion, Status: ScannerError}, ErrScannerUnavailable
	}
	buf := make([]byte, clamAVChunkBytes)
	var total int64
	for {
		n, readErr := source.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > s.config.MaxBytes || total > request.SizeBytes {
				return ScanResult{}, ErrSecurityRejected
			}
			var header [4]byte
			binary.BigEndian.PutUint32(header[:], uint32(n))
			if _, err := conn.Write(header[:]); err != nil {
				return ScanResult{ScannerName: "clamav", EngineVersion: s.config.EngineVersion, SignatureVersion: s.config.SignatureVersion, Status: ScannerError}, ErrScannerUnavailable
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return ScanResult{ScannerName: "clamav", EngineVersion: s.config.EngineVersion, SignatureVersion: s.config.SignatureVersion, Status: ScannerError}, ErrScannerUnavailable
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return ScanResult{}, fmt.Errorf("%w: scanner source", ErrStorage)
		}
	}
	if total != request.SizeBytes {
		return ScanResult{}, ErrSecurityRejected
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return ScanResult{ScannerName: "clamav", EngineVersion: s.config.EngineVersion, SignatureVersion: s.config.SignatureVersion, Status: ScannerError}, ErrScannerUnavailable
	}
	response, err := bufio.NewReader(io.LimitReader(conn, 4096)).ReadString('\x00')
	if err != nil && !errors.Is(err, io.EOF) {
		return ScanResult{ScannerName: "clamav", EngineVersion: s.config.EngineVersion, SignatureVersion: s.config.SignatureVersion, Status: ScannerError}, ErrScannerUnavailable
	}
	return parseClamAVResponse(response, s.config)
}

func parseClamAVResponse(response string, config ClamAVConfig) (ScanResult, error) {
	response = strings.TrimSpace(strings.TrimSuffix(response, "\x00"))
	base := ScanResult{ScannerName: "clamav", EngineVersion: config.EngineVersion, SignatureVersion: config.SignatureVersion}
	if response == "stream: OK" {
		base.Status = ScannerClean
		return base, nil
	}
	if strings.HasPrefix(response, "stream: ") && strings.HasSuffix(response, " FOUND") {
		found := strings.LastIndex(response, " FOUND")
		if found <= len("stream: ") {
			base.Status = ScannerError
			return base, ErrScannerUnavailable
		}
		signature := response[len("stream: "):found]
		digest := sha256Hex([]byte(signature))
		base.Status = ScannerInfected
		base.ThreatCode = "sig_" + digest[:24]
		return base, nil
	}
	base.Status = ScannerError
	return base, ErrScannerUnavailable
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return fmt.Sprintf("%x", sum[:])
}
