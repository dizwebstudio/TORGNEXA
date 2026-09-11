package securityedge

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const valkeyLimitScript = `local now=redis.call('TIME')
local nowms=(now[1]*1000)+math.floor(now[2]/1000)
local window=tonumber(ARGV[1])
local maximum=tonumber(ARGV[2])
local maxkeys=tonumber(ARGV[3])
if redis.call('EXISTS',KEYS[1])==0 then
  redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',nowms)
  if redis.call('ZCARD',KEYS[2])>=maxkeys then return {-1,0,window} end
  redis.call('ZADD',KEYS[2],nowms+window,ARGV[4])
  redis.call('PEXPIRE',KEYS[2],window*2)
end
local count=redis.call('INCR',KEYS[1])
if count==1 then redis.call('PEXPIRE',KEYS[1],window) end
local ttl=redis.call('PTTL',KEYS[1])
if count>maximum then return {0,0,ttl} end
return {1,maximum-count,ttl}`

// ValkeyConfig bounds the distributed rate-limit adapter. Username selects an
// optional ACL identity; Password is secret-bearing process configuration and
// is never included in errors or keys.
type ValkeyConfig struct {
	Address        string
	Username       string
	Password       string
	Namespace      string
	ConnectTimeout time.Duration
	RequestTimeout time.Duration
	MaxConnections int
	MaxKeys        int
}

func (config ValkeyConfig) valid() bool {
	if len(config.Address) > 255 || len(config.Username) > 128 || len(config.Password) > 4096 || strings.IndexFunc(config.Username, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 || (config.Username != "" && config.Password == "") || len(config.Namespace) < 1 || len(config.Namespace) > 64 || strings.ContainsAny(config.Namespace, "{}") || strings.IndexFunc(config.Namespace, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 || config.ConnectTimeout < 50*time.Millisecond || config.ConnectTimeout > 5*time.Second || config.RequestTimeout < 50*time.Millisecond || config.RequestTimeout > 5*time.Second || config.MaxConnections < 1 || config.MaxConnections > 256 || config.MaxKeys < 1 || config.MaxKeys > 1_000_000 {
		return false
	}
	host, port, err := net.SplitHostPort(config.Address)
	if err != nil || host == "" || strings.ContainsAny(host, "\r\n\t ") {
		return false
	}
	value, err := strconv.Atoi(port)
	return err == nil && value >= 1 && value <= 65535
}

type valkeyConnection struct {
	connection net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
}

// ValkeyLimiter uses one atomic Lua evaluation per request. Separate limiter
// instances share counters through Valkey; a bounded connection pool prevents
// one process-wide connection from serializing the request path.
type ValkeyLimiter struct {
	config  ValkeyConfig
	idle    chan *valkeyConnection
	slots   chan struct{}
	closed  chan struct{}
	close   sync.Once
	metrics limiterMetrics
	stopped atomic.Bool
}

// NewValkeyLimiter verifies the configured backend before returning a limiter.
func NewValkeyLimiter(ctx context.Context, config ValkeyConfig) (*ValkeyLimiter, error) {
	if ctx == nil || !config.valid() {
		return nil, ErrInvalid
	}
	limiter := &ValkeyLimiter{config: config, idle: make(chan *valkeyConnection, config.MaxConnections), slots: make(chan struct{}, config.MaxConnections), closed: make(chan struct{})}
	requestCtx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	connection, err := limiter.acquire(requestCtx)
	if err != nil {
		_ = limiter.Close()
		return nil, errors.Join(ErrLimiterUnavailable, err)
	}
	err = connection.command(requestCtx, config.RequestTimeout, "PING")
	limiter.release(connection, err == nil)
	if err != nil {
		_ = limiter.Close()
		return nil, errors.Join(ErrLimiterUnavailable, err)
	}
	return limiter, nil
}

// Allow atomically consumes one distributed budget unit.
func (limiter *ValkeyLimiter) Allow(ctx context.Context, limit Limit) (Decision, error) {
	if limiter == nil || ctx == nil || !limit.valid() {
		return Decision{}, ErrInvalid
	}
	if limiter.stopped.Load() {
		limiter.metrics.unavailable.Add(1)
		return Decision{}, ErrLimiterUnavailable
	}
	requestCtx, cancel := context.WithTimeout(ctx, limiter.config.RequestTimeout)
	defer cancel()
	connection, err := limiter.acquire(requestCtx)
	if err != nil {
		limiter.metrics.unavailable.Add(1)
		return Decision{}, errors.Join(ErrLimiterUnavailable, err)
	}
	digest := limitDigest(limit)
	counterKey, activeKey, digestText := valkeyKeys(limiter.config.Namespace, limit.Name, digest)
	values, err := connection.integerArray(requestCtx, limiter.config.RequestTimeout,
		"EVAL", valkeyLimitScript, "2", counterKey, activeKey,
		strconv.FormatInt(limit.Window.Milliseconds(), 10), strconv.Itoa(limit.Max), strconv.Itoa(limiter.config.MaxKeys), digestText)
	limiter.release(connection, err == nil)
	if err != nil || len(values) != 3 {
		limiter.metrics.unavailable.Add(1)
		if err == nil {
			err = errors.New("unexpected valkey response")
		}
		return Decision{}, errors.Join(ErrLimiterUnavailable, err)
	}
	retryAfter := positiveDuration(time.Duration(values[2]) * time.Millisecond)
	if values[0] < 1 {
		limiter.metrics.limited.Add(1)
		if values[0] < 0 {
			limiter.metrics.capacity.Add(1)
		}
		return Decision{RetryAfter: retryAfter}, ErrRateLimited
	}
	limiter.metrics.allowed.Add(1)
	return Decision{Remaining: int(values[1])}, nil
}

func valkeyKeys(namespace, limitName string, digest [32]byte) (counter, active, member string) {
	member = hex.EncodeToString(digest[:])
	hashTag := namespace + ":" + limitName
	return namespace + ":{" + hashTag + "}:counter:" + member, namespace + ":{" + hashTag + "}:active", member
}

// Metrics returns label-free process-local outcome counters.
func (limiter *ValkeyLimiter) Metrics() LimiterMetrics { return limiter.metrics.snapshot() }

// Close prevents new operations and releases pooled connections.
func (limiter *ValkeyLimiter) Close() error {
	if limiter == nil {
		return nil
	}
	limiter.close.Do(func() {
		limiter.stopped.Store(true)
		close(limiter.closed)
		for {
			select {
			case connection := <-limiter.idle:
				_ = connection.connection.Close()
				<-limiter.slots
			default:
				return
			}
		}
	})
	return nil
}

func (limiter *ValkeyLimiter) acquire(ctx context.Context) (*valkeyConnection, error) {
	select {
	case <-limiter.closed:
		return nil, errors.New("limiter closed")
	case connection := <-limiter.idle:
		return connection, nil
	default:
	}
	select {
	case limiter.slots <- struct{}{}:
		connection, err := limiter.dial(ctx)
		if err != nil {
			<-limiter.slots
			return nil, err
		}
		return connection, nil
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-limiter.closed:
		return nil, errors.New("limiter closed")
	case connection := <-limiter.idle:
		return connection, nil
	}
}

func (limiter *ValkeyLimiter) dial(ctx context.Context) (*valkeyConnection, error) {
	dialer := net.Dialer{Timeout: limiter.config.ConnectTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", limiter.config.Address)
	if err != nil {
		return nil, err
	}
	connection := &valkeyConnection{connection: raw, reader: bufio.NewReaderSize(raw, 4096), writer: bufio.NewWriterSize(raw, 4096)}
	if limiter.config.Password != "" {
		auth := []string{"AUTH", limiter.config.Password}
		if limiter.config.Username != "" {
			auth = []string{"AUTH", limiter.config.Username, limiter.config.Password}
		}
		if err := connection.command(ctx, limiter.config.RequestTimeout, auth...); err != nil {
			_ = raw.Close()
			return nil, err
		}
	}
	return connection, nil
}

func (limiter *ValkeyLimiter) release(connection *valkeyConnection, reusable bool) {
	if connection == nil {
		return
	}
	if !reusable || limiter.stopped.Load() {
		_ = connection.connection.Close()
		<-limiter.slots
		return
	}
	_ = connection.connection.SetDeadline(time.Time{})
	select {
	case limiter.idle <- connection:
	default:
		_ = connection.connection.Close()
		<-limiter.slots
	}
}

func (connection *valkeyConnection) command(ctx context.Context, timeout time.Duration, arguments ...string) error {
	if err := connection.write(ctx, timeout, arguments...); err != nil {
		return err
	}
	prefix, _, err := connection.responseLine()
	if err != nil {
		return err
	}
	if prefix != '+' {
		return errors.New("unexpected valkey response")
	}
	return nil
}

func (connection *valkeyConnection) integerArray(ctx context.Context, timeout time.Duration, arguments ...string) ([]int64, error) {
	if err := connection.write(ctx, timeout, arguments...); err != nil {
		return nil, err
	}
	prefix, rawLength, err := connection.responseLine()
	if err != nil || prefix != '*' {
		return nil, errors.New("unexpected valkey response")
	}
	length, err := strconv.Atoi(rawLength)
	if err != nil || length < 0 || length > 8 {
		return nil, errors.New("unexpected valkey response")
	}
	values := make([]int64, length)
	for index := range values {
		prefix, raw, err := connection.responseLine()
		if err != nil || prefix != ':' {
			return nil, errors.New("unexpected valkey response")
		}
		values[index], err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, errors.New("unexpected valkey response")
		}
	}
	return values, nil
}

func (connection *valkeyConnection) write(ctx context.Context, timeout time.Duration, arguments ...string) error {
	if connection == nil || connection.connection == nil || len(arguments) == 0 {
		return ErrInvalid
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.connection.SetDeadline(deadline); err != nil {
		return err
	}
	if _, err := connection.writer.WriteString("*" + strconv.Itoa(len(arguments)) + "\r\n"); err != nil {
		return err
	}
	for _, argument := range arguments {
		if _, err := connection.writer.WriteString("$" + strconv.Itoa(len(argument)) + "\r\n" + argument + "\r\n"); err != nil {
			return err
		}
	}
	return connection.writer.Flush()
}

func (connection *valkeyConnection) responseLine() (byte, string, error) {
	line, err := connection.reader.ReadSlice('\n')
	if err != nil {
		return 0, "", err
	}
	if len(line) < 3 || line[len(line)-2] != '\r' || line[len(line)-1] != '\n' {
		return 0, "", errors.New("unexpected valkey response")
	}
	if line[0] == '-' {
		return 0, "", errors.New("valkey rejected command")
	}
	return line[0], string(line[1 : len(line)-2]), nil
}

var _ io.Closer = (*ValkeyLimiter)(nil)
