// Copyright 2015-2017 HenryLee. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tp

import (
	"context"
	"crypto/tls"
	"os"
	"sync"
	"time"

	"github.com/henrylee2cn/goutil/pool"
	"github.com/henrylee2cn/teleport/socket"
	"github.com/henrylee2cn/teleport/utils"
)

// Packet types
const (
	TypeUndefined byte = 0
	TypePull      byte = 1
	TypeReply     byte = 2 // reply to pull
	TypePush      byte = 3
)

// TypeText returns the packet type text.
// If the type is undefined returns 'Undefined'.
func TypeText(typ byte) string {
	switch typ {
	case TypePull:
		return "PULL"
	case TypeReply:
		return "REPLY"
	case TypePush:
		return "PUSH"
	default:
		return "Undefined"
	}
}

// Internal Framework Rerror code.
// Note: Recommended custom code is greater than 1000.
const (
	CodeUnknownError    = -1
	CodeDialFailed      = 105
	CodeConnClosed      = 102
	CodeWriteFailed     = 104
	CodeBadPacket       = 400
	CodeNotFound        = 404
	CodePtypeNotAllowed = 405
	CodeHandleTimeout   = 408

	// CodeConflict                      = 409
	// CodeUnsupportedTx                 = 410
	// CodeUnsupportedCodecType          = 415
	// CodeUnauthorized                  = 401
	// CodeInternalServerError           = 500
	// CodeBadGateway                    = 502
	// CodeServiceUnavailable            = 503
	// CodeGatewayTimeout                = 504
	// CodeVariantAlsoNegotiates         = 506
	// CodeInsufficientStorage           = 507
	// CodeLoopDetected                  = 508
	// CodeNotExtended                   = 510
	// CodeNetworkAuthenticationRequired = 511
)

// CodeText returns the reply error code text.
// If the type is undefined returns 'Unknown Error'.
func CodeText(rerrCode int32) string {
	switch rerrCode {
	case CodeDialFailed:
		return "Dial Failed"
	case CodeConnClosed:
		return "Connection Closed"
	case CodeWriteFailed:
		return "Write Failed"
	case CodeBadPacket:
		return "Bad Packet"
	case CodeNotFound:
		return "Not Found"
	case CodeHandleTimeout:
		return "Handle Timeout"
	case CodePtypeNotAllowed:
		return "Packet Type Not Allowed"
	case CodeUnknownError:
		fallthrough
	default:
		return "Unknown Error"
	}
}

// Internal Framework Rerror string.
var (
	rerrUnknownError        = NewRerror(CodeUnknownError, CodeText(CodeUnknownError), "")
	rerrDialFailed          = NewRerror(CodeDialFailed, CodeText(CodeDialFailed), "")
	rerrConnClosed          = NewRerror(CodeConnClosed, CodeText(CodeConnClosed), "")
	rerrWriteFailed         = NewRerror(CodeWriteFailed, CodeText(CodeWriteFailed), "")
	rerrBadPacket           = NewRerror(CodeBadPacket, CodeText(CodeBadPacket), "")
	rerrNotFound            = NewRerror(CodeNotFound, CodeText(CodeNotFound), "")
	rerrCodePtypeNotAllowed = NewRerror(CodePtypeNotAllowed, CodeText(CodePtypeNotAllowed), "")
	rerrHandleTimeout       = NewRerror(CodeHandleTimeout, CodeText(CodeHandleTimeout), "")
)

// IsConnRerror determines whether the error is a connection error
func IsConnRerror(rerr *Rerror) bool {
	if rerr == nil {
		return false
	}
	if rerr.Code == CodeDialFailed || rerr.Code == CodeConnClosed {
		return true
	}
	return false
}

const (
	// MetaRerrorKey reply error metadata key
	MetaRerrorKey = "X-Reply-Error"
	// MetaRealId real ID metadata key
	MetaRealId = "X-Real-ID"
	// MetaRealIp real IP metadata key
	MetaRealIp = "X-Real-IP"
)

// WithRealId sets the real ID to metadata.
func WithRealId(id string) socket.PacketSetting {
	return socket.WithAddMeta(MetaRealId, id)
}

// WithRealIp sets the real IP to metadata.
func WithRealIp(ip string) socket.PacketSetting {
	return socket.WithAddMeta(MetaRealIp, ip)
}

// WithContext sets the packet handling context.
//  func WithContext(ctx context.Context) socket.PacketSetting
var WithContext = socket.WithContext

// WithSeq sets the packet sequence.
//  func WithSeq(seq uint64) socket.PacketSetting
var WithSeq = socket.WithSeq

// WithPtype sets the packet type.
//  func WithPtype(ptype byte) socket.PacketSetting
var WithPtype = socket.WithPtype

// WithUri sets the packet URL string.
//  func WithUri(uri string) socket.PacketSetting
var WithUri = socket.WithUri

// WithQuery sets the packet URL query parameter.
//  func WithQuery(key, value string) socket.PacketSetting
var WithQuery = socket.WithQuery

// WithAddMeta adds 'key=value' metadata argument.
// Multiple values for the same key may be added.
//  func WithAddMeta(key, value string) socket.PacketSetting
var WithAddMeta = socket.WithAddMeta

// WithSetMeta sets 'key=value' metadata argument.
//  func WithSetMeta(key, value string) socket.PacketSetting
var WithSetMeta = socket.WithSetMeta

// WithBodyCodec sets the body codec.
//  func WithBodyCodec(bodyCodec byte) socket.PacketSetting
var WithBodyCodec = socket.WithBodyCodec

// WithBody sets the body object.
//  func WithBody(body interface{}) socket.PacketSetting
var WithBody = socket.WithBody

// WithNewBody resets the function of geting body.
//  func WithNewBody(newBodyFunc socket.NewBodyFunc) socket.PacketSetting
var WithNewBody = socket.WithNewBody

// WithXferPipe sets transfer filter pipe.
//  func WithXferPipe(filterId ...byte) socket.PacketSetting
var WithXferPipe = socket.WithXferPipe

// GetPacket gets a *Packet form packet stack.
// Note:
//  newBodyFunc is only for reading form connection;
//  settings are only for writing to connection.
//  func GetPacket(settings ...socket.PacketSetting) *socket.Packet
var GetPacket = socket.GetPacket

// PutPacket puts a *socket.Packet to packet stack.
//  func PutPacket(p *socket.Packet)
var PutPacket = socket.PutPacket

var (
	_maxGoroutinesAmount      = (1024 * 1024 * 8) / 8 // max memory 8GB (8KB/goroutine)
	_maxGoroutineIdleDuration time.Duration
	_gopool                   = pool.NewGoPool(_maxGoroutinesAmount, _maxGoroutineIdleDuration)
)

// SetGopool set or reset go pool config.
// Note: Make sure to call it before calling NewPeer() and Go()
func SetGopool(maxGoroutinesAmount int, maxGoroutineIdleDuration time.Duration) {
	_maxGoroutinesAmount, _maxGoroutineIdleDuration := maxGoroutinesAmount, maxGoroutineIdleDuration
	if _gopool != nil {
		_gopool.Stop()
	}
	_gopool = pool.NewGoPool(_maxGoroutinesAmount, _maxGoroutineIdleDuration)
}

// Go similar to go func, but return false if insufficient resources.
func Go(fn func()) bool {
	if err := _gopool.Go(fn); err != nil {
		Warnf("%s", err.Error())
		return false
	}
	return true
}

// AnywayGo similar to go func, but concurrent resources are limited.
func AnywayGo(fn func()) {
TRYGO:
	if !Go(fn) {
		time.Sleep(time.Second)
		goto TRYGO
	}
}

var printPidOnce sync.Once

func doPrintPid() {
	printPidOnce.Do(func() {
		Printf("The current process PID: %d", os.Getpid())
	})
}

type fakePullCmd struct {
	output    *socket.Packet
	reply     interface{}
	rerr      *Rerror
	inputMeta *utils.Args
}

// NewFakePullCmd creates a fake PullCmd.
func NewFakePullCmd(uri string, args, reply interface{}, rerr *Rerror) PullCmd {
	return &fakePullCmd{
		output: socket.NewPacket(
			socket.WithPtype(TypePull),
			socket.WithUri(uri),
			socket.WithBody(args),
		),
		reply:     reply,
		rerr:      rerr,
		inputMeta: utils.AcquireArgs(),
	}
}

// Output returns writed packet.
func (f *fakePullCmd) Output() *socket.Packet {
	return f.output
}

// Context carries a deadline, a cancelation signal, and other values across
// API boundaries.
func (f *fakePullCmd) Context() context.Context {
	return f.output.Context()
}

// Result returns the pull result.
func (f *fakePullCmd) Result() (interface{}, *Rerror) {
	return f.reply, f.rerr
}

// Rerror returns the pull error.
func (f *fakePullCmd) Rerror() *Rerror {
	return f.rerr
}

// InputMeta returns the header metadata of input packet.
func (f *fakePullCmd) InputMeta() *utils.Args {
	return f.inputMeta
}

// CostTime returns the pulled cost time.
// If PeerConfig.CountTime=false, always returns 0.
func (f *fakePullCmd) CostTime() time.Duration {
	return 0
}

// NewTlsConfigFromFile creates a new TLS config.
func NewTlsConfigFromFile(tlsCertFile, tlsKeyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates:             []tls.Certificate{cert},
		NextProtos:               []string{"http/1.1", "h2"},
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		},
	}, nil
}
