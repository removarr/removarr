// Package app builds provider clients from the current settings.
package app

import (
	"context"
	"net/http"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/providers/jellyfin"
	"github.com/removarr/removarr/internal/providers/qbittorrent"
	"github.com/removarr/removarr/internal/providers/radarr"
	"github.com/removarr/removarr/internal/providers/sonarr"
	"github.com/removarr/removarr/internal/scanner"
)

// Apps builds clients for the apps removarr talks to. Settings are read on every call, so
// a change made through the API takes effect on the next scan.
type Apps struct {
	Config  *config.Service
	Timeout time.Duration // limit for one request to an app
}

func (a *Apps) client() *http.Client {
	return &http.Client{Timeout: a.Timeout, CheckRedirect: httpjson.SameHostRedirects}
}

// Providers returns a client for every configured app. The others stay nil.
func (a *Apps) Providers(ctx context.Context) (scanner.Providers, error) {
	var p scanner.Providers

	c, ok, err := a.Config.App(ctx, config.AppJellyfin)
	if err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		p.MediaServer = jellyfin.New(c.URL, c.APIKey, a.client())
	}

	if c, ok, err = a.Config.App(ctx, config.AppRadarr); err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		p.Movies = radarr.New(c.URL, c.APIKey, a.client())
	}

	if c, ok, err = a.Config.App(ctx, config.AppSonarr); err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		p.Series = sonarr.New(c.URL, c.APIKey, a.client())
	}

	if c, ok, err = a.Config.App(ctx, config.AppQBittorrent); err != nil {
		return scanner.Providers{}, err
	}
	if ok {
		client, err := qbittorrent.New(c.URL, c.Username, c.Password, a.Timeout)
		if err != nil {
			return scanner.Providers{}, err
		}
		p.Downloads = client
	}
	return p, nil
}

// Tester returns the connection tester of one app. It returns providers.ErrNotConfigured
// when the app is unknown or its settings are incomplete.
func (a *Apps) Tester(ctx context.Context, app string) (providers.Tester, error) {
	c, ok, err := a.Config.App(ctx, app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, providers.ErrNotConfigured
	}
	switch app {
	case config.AppJellyfin:
		return jellyfin.New(c.URL, c.APIKey, a.client()), nil
	case config.AppRadarr:
		return radarr.New(c.URL, c.APIKey, a.client()), nil
	case config.AppSonarr:
		return sonarr.New(c.URL, c.APIKey, a.client()), nil
	case config.AppQBittorrent:
		client, err := qbittorrent.New(c.URL, c.Username, c.Password, a.Timeout)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, providers.ErrNotConfigured
	}
}

// MediaServer returns the media server client, or providers.ErrNotConfigured.
func (a *Apps) MediaServer(ctx context.Context) (providers.MediaServer, error) {
	c, ok, err := a.Config.App(ctx, config.AppJellyfin)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, providers.ErrNotConfigured
	}
	return jellyfin.New(c.URL, c.APIKey, a.client()), nil
}
