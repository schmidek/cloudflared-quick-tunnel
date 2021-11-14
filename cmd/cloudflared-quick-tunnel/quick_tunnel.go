package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	cli "github.com/urfave/cli/v2"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/connection"
)

const httpTimeout = 15 * time.Second

const disclaimer = "Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to" +
	" experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee. If you " +
	"intend to use Tunnels in production you should use a pre-created named tunnel by following: " +
	"https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"

// RunPersistentQuickTunnel requests a tunnel from the specified service.
// We use this to power quick tunnels on trycloudflare.com, but the
// service is open-source and could be used by anyone.
func RunPersistentQuickTunnel(c *cli.Context, log *zerolog.Logger, version string) error {
	var config *QuickTunnelConfig
	configFile := c.String("credentials")
	log.Info().Msg("Using config file: " + configFile)
	existingTunnel := false
	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		// config does not exist
		config, err = RequestNewQuickTunnel(c, log)
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}

		log.Info().Msg("Notifying server of changed tunnel")
		callbackOperation := func() error {
			resp, err := http.Post(fmt.Sprintf("%s/%s", c.String("url"), c.String("callback")), "text/plain", strings.NewReader(config.URL))
			if err != nil {
				return err
			}
			if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
				return nil
			} else {
				return errors.New("Callback error")
			}
		}
		err := backoff.Retry(callbackOperation, backoff.NewExponentialBackOff())
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}

		file, _ := json.MarshalIndent(config, "", " ")
		err = ioutil.WriteFile(configFile, file, 0644)
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}
	} else {
		byteValue, _ := ioutil.ReadFile(configFile)
		json.Unmarshal(byteValue, &config)
		existingTunnel = true
	}

	log.Info().Msg("Using: " + config.URL)

	if !c.IsSet("protocol") {
		c.Set("protocol", "quic")
	}

	err := tunnel.StartServer(
		c,
		version,
		&connection.NamedTunnelConfig{Credentials: config.Credentials, QuickTunnelUrl: config.URL},
		log,
		false,
	)
	if err == nil || !existingTunnel {
		return err
	}
	// Delete existing config and try again
	deleteErr := os.Remove(configFile)
	if deleteErr != nil {
		log.Error().Msg(deleteErr.Error())
		return deleteErr
	}

	// The following doesn't work because of prometheus duplicate metrics collector registration attempted
	// For now let's just return an error and have the process restarted by systemd or the like
	//return RunPersistentQuickTunnel(c, log, version)
	return errors.New("Failed to start server. Restart to create new tunnel.")
}

func RequestNewQuickTunnel(c *cli.Context, log *zerolog.Logger) (*QuickTunnelConfig, error) {
	log.Info().Msg(disclaimer)
	log.Info().Msg("Requesting new quick Tunnel on trycloudflare.com...")

	client := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   httpTimeout,
			ResponseHeaderTimeout: httpTimeout,
		},
		Timeout: httpTimeout,
	}

	resp, err := client.Post(fmt.Sprintf("%s/tunnel", c.String("quick-service")), "application/json", nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to request quick Tunnel")
	}
	defer resp.Body.Close()

	var data QuickTunnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quick Tunnel")
	}

	tunnelID, err := uuid.Parse(data.Result.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse quick Tunnel ID")
	}

	credentials := connection.Credentials{
		AccountTag:   data.Result.AccountTag,
		TunnelSecret: data.Result.Secret,
		TunnelID:     tunnelID,
		TunnelName:   data.Result.Name,
	}

	url := data.Result.Hostname
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	for _, line := range AsciiBox([]string{
		"Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
		url,
	}, 2) {
		log.Info().Msg(line)
	}

	return &QuickTunnelConfig{URL: data.Result.Hostname, Credentials: credentials}, nil
}

type QuickTunnelConfig struct {
	URL         string
	Credentials connection.Credentials
}

type QuickTunnelResponse struct {
	Success bool
	Result  QuickTunnel
	Errors  []QuickTunnelError
}

type QuickTunnelError struct {
	Code    int
	Message string
}

type QuickTunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	AccountTag string `json:"account_tag"`
	Secret     []byte `json:"secret"`
}

// Print out the given lines in a nice ASCII box.
func AsciiBox(lines []string, padding int) (box []string) {
	maxLen := maxLen(lines)
	spacer := strings.Repeat(" ", padding)
	border := "+" + strings.Repeat("-", maxLen+(padding*2)) + "+"
	box = append(box, border)
	for _, line := range lines {
		box = append(box, "|"+spacer+line+strings.Repeat(" ", maxLen-len(line))+spacer+"|")
	}
	box = append(box, border)
	return
}

func maxLen(lines []string) int {
	max := 0
	for _, line := range lines {
		if len(line) > max {
			max = len(line)
		}
	}
	return max
}
