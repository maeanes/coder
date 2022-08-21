package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/xerrors"

	"github.com/coder/coder/peer"
	"github.com/coder/coder/peerbroker/proto"
	"github.com/coder/coder/tailnet"
)

// ReconnectingPTYRequest is sent from the client to the server
// to pipe data to a PTY.
type ReconnectingPTYRequest struct {
	Data   string `json:"data"`
	Height uint16 `json:"height"`
	Width  uint16 `json:"width"`
}

// Conn is a temporary interface while we switch from WebRTC to Wireguard networking.
type Conn interface {
	io.Closer
	Closed() <-chan struct{}
	Ping() (time.Duration, error)
	CloseWithError(err error) error
	ReconnectingPTY(id string, height, width uint16, command string) (net.Conn, error)
	SSH() (net.Conn, error)
	SSHClient() (*ssh.Client, error)
	DialContext(ctx context.Context, network string, addr string) (net.Conn, error)
}

// Conn wraps a peer connection with helper functions to
// communicate with the agent.
type WebRTCConn struct {
	// Negotiator is responsible for exchanging messages.
	Negotiator proto.DRPCPeerBrokerClient

	*peer.Conn
}

// ReconnectingPTY returns a connection serving a TTY that can
// be reconnected to via ID.
//
// The command is optional and defaults to start a shell.
func (c *WebRTCConn) ReconnectingPTY(id string, height, width uint16, command string) (net.Conn, error) {
	channel, err := c.CreateChannel(context.Background(), fmt.Sprintf("%s:%d:%d:%s", id, height, width, command), &peer.ChannelOptions{
		Protocol: ProtocolReconnectingPTY,
	})
	if err != nil {
		return nil, xerrors.Errorf("pty: %w", err)
	}
	return channel.NetConn(), nil
}

// SSH dials the built-in SSH server.
func (c *WebRTCConn) SSH() (net.Conn, error) {
	channel, err := c.CreateChannel(context.Background(), "ssh", &peer.ChannelOptions{
		Protocol: ProtocolSSH,
	})
	if err != nil {
		return nil, xerrors.Errorf("dial: %w", err)
	}
	return channel.NetConn(), nil
}

// SSHClient calls SSH to create a client that uses a weak cipher
// for high throughput.
func (c *WebRTCConn) SSHClient() (*ssh.Client, error) {
	netConn, err := c.SSH()
	if err != nil {
		return nil, xerrors.Errorf("ssh: %w", err)
	}
	sshConn, channels, requests, err := ssh.NewClientConn(netConn, "localhost:22", &ssh.ClientConfig{
		// SSH host validation isn't helpful, because obtaining a peer
		// connection already signifies user-intent to dial a workspace.
		// #nosec
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		return nil, xerrors.Errorf("ssh conn: %w", err)
	}
	return ssh.NewClient(sshConn, channels, requests), nil
}

// DialContext dials an arbitrary protocol+address from inside the workspace and
// proxies it through the provided net.Conn.
func (c *WebRTCConn) DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	u := &url.URL{
		Scheme: network,
	}
	if strings.HasPrefix(network, "unix") {
		u.Path = addr
	} else {
		u.Host = addr
	}

	channel, err := c.CreateChannel(ctx, u.String(), &peer.ChannelOptions{
		Protocol:  ProtocolDial,
		Unordered: strings.HasPrefix(network, "udp"),
	})
	if err != nil {
		return nil, xerrors.Errorf("create datachannel: %w", err)
	}

	// The first message written from the other side is a JSON payload
	// containing the dial error.
	dec := json.NewDecoder(channel)
	var res dialResponse
	err = dec.Decode(&res)
	if err != nil {
		return nil, xerrors.Errorf("decode agent dial response: %w", err)
	}
	if res.Error != "" {
		_ = channel.Close()
		return nil, xerrors.Errorf("remote dial error: %v", res.Error)
	}

	return channel.NetConn(), nil
}

func (c *WebRTCConn) Close() error {
	_ = c.Negotiator.DRPCConn().Close()
	return c.Conn.Close()
}

type TailnetConn struct {
	*tailnet.Conn
}

func (c *TailnetConn) Ping() (time.Duration, error) {
	return 0, nil
}

func (c *TailnetConn) CloseWithError(err error) error {
	return c.Close()
}

type reconnectingPTYInit struct {
	ID      string
	Height  uint16
	Width   uint16
	Command string
}

func (c *TailnetConn) ReconnectingPTY(id string, height, width uint16, command string) (net.Conn, error) {
	return c.DialContextTCP(context.Background(), netip.AddrPortFrom(tailnetIP, uint16(tailnetReconnectingPTYPort)))
}

func (c *TailnetConn) SSH() (net.Conn, error) {
	return c.DialContextTCP(context.Background(), netip.AddrPortFrom(tailnetIP, uint16(tailnetSSHPort)))
}

// SSHClient calls SSH to create a client that uses a weak cipher
// for high throughput.
func (c *TailnetConn) SSHClient() (*ssh.Client, error) {
	netConn, err := c.SSH()
	if err != nil {
		return nil, xerrors.Errorf("ssh: %w", err)
	}
	sshConn, channels, requests, err := ssh.NewClientConn(netConn, "localhost:22", &ssh.ClientConfig{
		// SSH host validation isn't helpful, because obtaining a peer
		// connection already signifies user-intent to dial a workspace.
		// #nosec
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		return nil, xerrors.Errorf("ssh conn: %w", err)
	}
	return ssh.NewClient(sshConn, channels, requests), nil
}

func (c *TailnetConn) DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	_, rawPort, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(rawPort)
	return c.Conn.DialContextTCP(ctx, netip.AddrPortFrom(tailnetIP, uint16(port)))
}
