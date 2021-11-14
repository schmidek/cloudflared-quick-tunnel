package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/getsentry/raven-go"
	cli "github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.uber.org/automaxprocs/maxprocs"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

const (
	versionText = "Print the version"
)

var (
	Version   = "DEV"
	BuildTime = "unknown"
	// Mostly network errors that we don't want reported back to Sentry, this is done by substring match.
	ignoredErrors = []string{
		"connection reset by peer",
		"An existing connection was forcibly closed by the remote host.",
		"use of closed connection",
		"You need to enable Argo Smart Routing",
		"3001 connection closed",
		"3002 connection dropped",
		"rpc exception: dial tcp",
		"rpc exception: EOF",
	}
)

func main() {
	rand.Seed(time.Now().UnixNano())
	metrics.RegisterBuildInfo(BuildTime, Version)
	raven.SetRelease(Version)
	maxprocs.Set()

	// Graceful shutdown channel used by the app. When closed, app must terminate gracefully.
	// Windows service manager closes this channel when it receives stop command.
	graceShutdownC := make(chan struct{})

	cli.VersionFlag = &cli.BoolFlag{
		Name:    "version",
		Aliases: []string{"v", "V"},
		Usage:   versionText,
	}

	app := &cli.App{}
	app.Name = "cloudflared-quick-tunnel"
	app.Usage = "Creates a Cloudflare quick tunnel"
	app.UsageText = "cloudflared-quick-tunnel [global options] [command] [command options]"
	app.Version = fmt.Sprintf("%s (built %s)", Version, BuildTime)
	app.Description = `Creates a Cloudflare quick tunnel, maintains the credentials and notifies when the url of the tunnel changes`
	//app.Flags = flags()
	//app.Action = action(graceShutdownC)
	app.Commands = commands(cli.ShowVersion)

	tunnel.Init(Version, graceShutdownC) // we need this to support the tunnel sub command...
	//access.Init(graceShutdownC)
	//updater.Init(Version)
	runApp(app, graceShutdownC)
}

func commands(version func(c *cli.Context)) []*cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "credentials",
			Usage:   "specify a version you wish to upgrade or downgrade to",
			Hidden:  false,
			Value:   "./credentials.json",
			EnvVars: []string{"TUNNEL_CONFIG"},
		},
		&cli.StringFlag{
			Name:    "callback",
			Usage:   "specify a version you wish to upgrade or downgrade to",
			Hidden:  false,
			EnvVars: []string{"CALLBACK"},
		},
	}
	flags = append(flags, configureProxyFlags(false)...)
	flags = append(flags, tunnelFlags(true)...)
	cmds := []*cli.Command{
		{
			Name: "run",
			Action: func(c *cli.Context) (err error) {
				log := logger.CreateLoggerFromContext(c, false)
				RunPersistentQuickTunnel(c, log, Version)
				return nil
			},
			Usage:       "Update the agent if a new version exists",
			Flags:       flags,
			Description: ``,
		},
		{
			Name: "version",
			Action: func(c *cli.Context) (err error) {
				version(c)
				return nil
			},
			Usage:       versionText,
			Description: versionText,
		},
	}
	return cmds
}

/*func action(graceShutdownC chan struct{}) cli.ActionFunc {
	return cliutil.ConfiguredAction(func(c *cli.Context) (err error) {
		tags := make(map[string]string)
		tags["hostname"] = c.String("hostname")
		raven.SetTagsContext(tags)
		raven.CapturePanic(func() { err = tunnel.TunnelCommand(c) }, nil)
		if err != nil {
			captureError(err)
		}
		return err
	})
}*/

// In order to keep the amount of noise sent to Sentry low, typical network errors can be filtered out here by a substring match.
func captureError(err error) {
	errorMessage := err.Error()
	for _, ignoredErrorMessage := range ignoredErrors {
		if strings.Contains(errorMessage, ignoredErrorMessage) {
			return
		}
	}
	raven.CaptureError(err, nil)
}

const (
	allSortByOptions     = "name, id, createdAt, deletedAt, numConnections"
	connsSortByOptions   = "id, startedAt, numConnections, version"
	CredFileFlagAlias    = "cred-file"
	CredFileFlag         = "credentials-file"
	CredContentsFlag     = "credentials-contents"
	overwriteDNSFlagName = "overwrite-dns"

	LogFieldTunnelID  = "tunnelID"
	debugLevelWarning = "At debug level cloudflared will log request URL, method, protocol, content length, as well as, all request and response headers. " +
		"This can expose sensitive information in your logs."
)

func tunnelFlags(shouldHide bool) []cli.Flag {
	flags := configureLoggingFlags(shouldHide)
	flags = append(flags, []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    CredFileFlag,
			Aliases: []string{CredFileFlagAlias},
			Usage:   "Filepath at which to read/write the tunnel credentials",
			EnvVars: []string{"TUNNEL_CRED_FILE"},
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "is-autoupdated",
			Usage:  "Signal the new process that Cloudflare Tunnel connector has been autoupdated",
			Value:  false,
			Hidden: true,
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "edge",
			Usage:   "Address of the Cloudflare tunnel server. Only works in Cloudflare's internal testing environment.",
			EnvVars: []string{"TUNNEL_EDGE"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "region",
			Usage:   "Cloudflare Edge region to connect to. Omit or set to empty to connect to the global region.",
			EnvVars: []string{"TUNNEL_REGION"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    tlsconfig.CaCertFlag,
			Usage:   "Certificate Authority authenticating connections with Cloudflare's edge network.",
			EnvVars: []string{"TUNNEL_CACERT"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "hostname",
			Usage:   "Set a hostname on a Cloudflare zone to route traffic through this tunnel.",
			EnvVars: []string{"TUNNEL_HOSTNAME"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "id",
			Usage:   "A unique identifier used to tie connections to this tunnel instance.",
			EnvVars: []string{"TUNNEL_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "lb-pool",
			Usage:   "The name of a (new/existing) load balancing pool to add this origin to.",
			EnvVars: []string{"TUNNEL_LB_POOL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-key",
			Usage:   "This parameter has been deprecated since version 2017.10.1.",
			EnvVars: []string{"TUNNEL_API_KEY"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-email",
			Usage:   "This parameter has been deprecated since version 2017.10.1.",
			EnvVars: []string{"TUNNEL_API_EMAIL"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-ca-key",
			Usage:   "This parameter has been deprecated since version 2017.10.1.",
			EnvVars: []string{"TUNNEL_API_CA_KEY"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-url",
			Usage:   "Base URL for Cloudflare API v4",
			EnvVars: []string{"TUNNEL_API_URL"},
			Value:   "https://api.cloudflare.com/client/v4",
			Hidden:  true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "metrics-update-freq",
			Usage:   "Frequency to update tunnel metrics",
			Value:   time.Second * 5,
			EnvVars: []string{"TUNNEL_METRICS_UPDATE_FREQ"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "tag",
			Usage:   "Custom tags used to identify this tunnel, in format `KEY=VALUE`. Multiple tags may be specified",
			EnvVars: []string{"TUNNEL_TAG"},
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "heartbeat-interval",
			Usage:  "Minimum idle time before sending a heartbeat.",
			Value:  time.Second * 5,
			Hidden: true,
		}),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "heartbeat-count",
			Usage:  "Minimum number of unacked heartbeats to send before closing the connection.",
			Value:  5,
			Hidden: true,
		}),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "retries",
			Value:   5,
			Usage:   "Maximum number of retries for connection/protocol errors.",
			EnvVars: []string{"TUNNEL_RETRIES"},
			Hidden:  shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "ha-connections",
			Value:  4,
			Hidden: true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "grace-period",
			Usage:   "When cloudflared receives SIGINT/SIGTERM it will stop accepting new requests, wait for in-progress requests to terminate, then shutdown. Waiting for in-progress requests will timeout after this grace period, or when a second SIGTERM/SIGINT is received.",
			Value:   time.Second * 30,
			EnvVars: []string{"TUNNEL_GRACE_PERIOD"},
			Hidden:  shouldHide,
		}),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "compression-quality",
			Value:   0,
			Usage:   "(beta) Use cross-stream compression instead HTTP compression. 0-off, 1-low, 2-medium, >=3-high.",
			EnvVars: []string{"TUNNEL_COMPRESSION_LEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "use-reconnect-token",
			Usage:   "Test reestablishing connections with the new 'reconnect token' flow.",
			Value:   true,
			EnvVars: []string{"TUNNEL_USE_RECONNECT_TOKEN"},
			Hidden:  true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "dial-edge-timeout",
			Usage:   "Maximum wait time to set up a connection with the edge",
			Value:   time.Second * 15,
			EnvVars: []string{"DIAL_EDGE_TIMEOUT"},
			Hidden:  true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "stdin-control",
			Usage:   "Control the process using commands sent through stdin",
			EnvVars: []string{"STDIN_CONTROL"},
			Hidden:  true,
			Value:   false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "name",
			Aliases: []string{"n"},
			EnvVars: []string{"TUNNEL_NAME"},
			Usage:   "Stable name to identify the tunnel. Using this flag will create, route and run a tunnel. For production usage, execute each command separately",
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:   "quick-service",
			Usage:  "URL for a service which manages unauthenticated 'quick' tunnels.",
			Value:  "https://api.trycloudflare.com",
			Hidden: true,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "max-fetch-size",
			Usage:   `The maximum number of results that cloudflared can fetch from Cloudflare API for any listing operations needed`,
			EnvVars: []string{"TUNNEL_MAX_FETCH_SIZE"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "protocol",
			Value:   "auto",
			Aliases: []string{"p"},
			Usage:   fmt.Sprintf("Protocol implementation to connect with Cloudflare's edge network. %s", connection.AvailableProtocolFlagMessage),
			EnvVars: []string{"TUNNEL_TRANSPORT_PROTOCOL"},
			Hidden:  true,
		}),
		&cli.BoolFlag{
			Name:    overwriteDNSFlagName,
			Aliases: []string{"f"},
			Usage:   `Overwrites existing DNS records with this hostname`,
			EnvVars: []string{"TUNNEL_FORCE_PROVISIONING_DNS"},
		},
	}...)

	return flags
}

func configureProxyFlags(shouldHide bool) []cli.Flag {
	flags := []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "url",
			Value:   "http://localhost:8080",
			Usage:   "Connect to the local webserver at `URL`.",
			EnvVars: []string{"TUNNEL_URL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "hello-world",
			Value:   false,
			Usage:   "Run Hello World Server",
			EnvVars: []string{"TUNNEL_HELLO_WORLD"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.Socks5Flag,
			Usage:   "specify if this tunnel is running as a SOCK5 Server",
			EnvVars: []string{"TUNNEL_SOCKS"},
			Value:   false,
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyConnectTimeoutFlag,
			Usage:  "HTTP proxy timeout for establishing a new connection",
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyTLSTimeoutFlag,
			Usage:  "HTTP proxy timeout for completing a TLS handshake",
			Value:  time.Second * 10,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyTCPKeepAliveFlag,
			Usage:  "HTTP proxy TCP keepalive duration",
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   ingress.ProxyNoHappyEyeballsFlag,
			Usage:  "HTTP proxy should disable \"happy eyeballs\" for IPv4/v6 fallback",
			Hidden: shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   ingress.ProxyKeepAliveConnectionsFlag,
			Usage:  "HTTP proxy maximum keepalive connection pool size",
			Value:  100,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyKeepAliveTimeoutFlag,
			Usage:  "HTTP proxy timeout for closing an idle connection",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-connection-timeout",
			Usage:  "DEPRECATED. No longer has any effect.",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-expect-continue-timeout",
			Usage:  "DEPRECATED. No longer has any effect.",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    ingress.HTTPHostHeaderFlag,
			Usage:   "Sets the HTTP Host header for the local webserver.",
			EnvVars: []string{"TUNNEL_HTTP_HOST_HEADER"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    ingress.OriginServerNameFlag,
			Usage:   "Hostname on the origin server certificate.",
			EnvVars: []string{"TUNNEL_ORIGIN_SERVER_NAME"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "unix-socket",
			Usage:   "Path to unix socket to use instead of --url",
			EnvVars: []string{"TUNNEL_UNIX_SOCKET"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    tlsconfig.OriginCAPoolFlag,
			Usage:   "Path to the CA for the certificate of your origin. This option should be used only if your certificate is not signed by Cloudflare.",
			EnvVars: []string{"TUNNEL_ORIGIN_CA_POOL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.NoTLSVerifyFlag,
			Usage:   "Disables TLS verification of the certificate presented by your origin. Will allow any certificate from the origin to be accepted. Note: The connection from your machine to Cloudflare's Edge is still encrypted.",
			EnvVars: []string{"NO_TLS_VERIFY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.NoChunkedEncodingFlag,
			Usage:   "Disables chunked transfer encoding; useful if you are running a WSGI server.",
			EnvVars: []string{"TUNNEL_NO_CHUNKED_ENCODING"},
			Hidden:  shouldHide,
		}),
	}
	return flags
}

func configureLoggingFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogLevelFlag,
			Value:   "info",
			Usage:   "Application logging level {debug, info, warn, error, fatal}. " + debugLevelWarning,
			EnvVars: []string{"TUNNEL_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogTransportLevelFlag,
			Aliases: []string{"proto-loglevel"}, // This flag used to be called proto-loglevel
			Value:   "info",
			Usage:   "Transport logging level(previously called protocol logging level) {debug, info, warn, error, fatal}",
			EnvVars: []string{"TUNNEL_PROTO_LOGLEVEL", "TUNNEL_TRANSPORT_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogFileFlag,
			Usage:   "Save application log to this file for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGFILE"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogDirectoryFlag,
			Usage:   "Save application log to this directory for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGDIRECTORY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "trace-output",
			Usage:   "Name of trace output file, generated when cloudflared stops.",
			EnvVars: []string{"TUNNEL_TRACE_OUTPUT"},
			Hidden:  shouldHide,
		}),
	}
}
