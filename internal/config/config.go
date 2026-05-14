// Package config loads olcrtc runtime configuration from YAML files.
//
// The YAML schema mirrors [session.Config]. Fields left unset in the file
// remain at their zero value, allowing CLI flags to fill them in. Use
// [Apply] to merge a parsed [File] onto an existing [session.Config];
// non-zero fields in the session config (typically populated from CLI flags)
// take precedence over the YAML values.
//
//nolint:tagliatelle // YAML keys are the documented config file schema.
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"gopkg.in/yaml.v3"
)

// ErrConfigNotFound is returned when a config file path is set but the file does not exist.
var ErrConfigNotFound = errors.New("config file not found")

// File is the on-disk YAML schema.
type File struct {
	Mode   string `yaml:"mode"`
	Link   string `yaml:"link"`
	Auth   Auth   `yaml:"auth"`
	Room   Room   `yaml:"room"`
	Crypto Crypto `yaml:"crypto"`
	Net    Net    `yaml:"net"`
	SOCKS        SOCKS        `yaml:"socks"`
	Engine       Engine       `yaml:"engine"`
	Video        Video        `yaml:"video"`
	VP8          VP8          `yaml:"vp8"`
	SEI          SEI          `yaml:"sei"`
	Gen          Gen          `yaml:"gen"`
	Subscription Subscription `yaml:"subscription"`
	Warp         Warp         `yaml:"warp"`
	Data         string       `yaml:"data"`
	Debug        bool         `yaml:"debug"`
	FFmpeg       string       `yaml:"ffmpeg"`
}

// Auth selects the auth provider.
type Auth struct {
	Provider string `yaml:"provider"` // telemost, jazz, wbstream, none
}

// Room identifies the conference room.
type Room struct {
	ID string `yaml:"id"`
}

// Crypto holds the shared secret used to authenticate and encrypt the tunnel.
type Crypto struct {
	Key string `yaml:"key"` // 64-char hex (32 bytes)
}

// Net groups network and transport selection.
type Net struct {
	Transport string `yaml:"transport"` // datachannel, videochannel, seichannel, vp8channel
	DNS       string `yaml:"dns"`
}

// SOCKS bundles SOCKS5 listener and outbound-proxy settings.
type SOCKS struct {
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	User      string `yaml:"user"`
	Pass      string `yaml:"pass"`
	ProxyAddr string `yaml:"proxy_addr"`
	ProxyPort int    `yaml:"proxy_port"`
	ProxyUser string `yaml:"proxy_user"`   // наша фича
	ProxyPass string `yaml:"proxy_pass"`   // наша фича
}

// Warp holds WARP proxy settings.
type Warp struct {
	ProxyAddr string `yaml:"proxy_addr"`
	ProxyPort int    `yaml:"proxy_port"`
}

// Subscription holds subscription server settings.
type Subscription struct {
	Enabled  bool   `yaml:"enabled"`
	Port     int    `yaml:"port"`
	DBPath   string `yaml:"db_path"`
	APIToken string `yaml:"api_token"`
}

// Engine selects a direct SFU connection when Auth.Provider is "none".
type Engine struct {
	Name  string `yaml:"name"` // livekit, goolom, salutejazz
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

// Video tunes the videochannel transport.
type Video struct {
	Width      int    `yaml:"width"`
	Height     int    `yaml:"height"`
	FPS        int    `yaml:"fps"`
	Bitrate    string `yaml:"bitrate"`
	HW         string `yaml:"hw"`
	QRSize     int    `yaml:"qr_size"`
	QRRecovery string `yaml:"qr_recovery"`
	Codec      string `yaml:"codec"`
	TileModule int    `yaml:"tile_module"`
	TileRS     int    `yaml:"tile_rs"`
}

// VP8 tunes the vp8channel transport.
type VP8 struct {
	FPS       int `yaml:"fps"`
	BatchSize int `yaml:"batch_size"`
}

// SEI tunes the seichannel transport.
type SEI struct {
	FPS          int `yaml:"fps"`
	BatchSize    int `yaml:"batch_size"`
	FragmentSize int `yaml:"fragment_size"`
	AckTimeoutMS int `yaml:"ack_timeout_ms"`
}

// Gen controls room-generation mode.
type Gen struct {
	Amount int `yaml:"amount"`
}

// Load parses a YAML file from disk.
func Load(path string) (File, error) {
	// #nosec G304 -- config path is an explicit CLI/user input.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{}, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return File{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return f, nil
}

// Apply merges f onto dst. CLI-set fields (non-zero values in dst) win;
// YAML values fill in the rest.
func Apply(dst session.Config, f File) session.Config {
	dst.Mode = pickString(dst.Mode, f.Mode)
	dst.Link = pickString(dst.Link, f.Link)
	dst.Transport = pickString(dst.Transport, f.Net.Transport)
	dst.Auth = pickString(dst.Auth, f.Auth.Provider)
	dst.Engine = pickString(dst.Engine, f.Engine.Name)
	dst.URL = pickString(dst.URL, f.Engine.URL)
	dst.Token = pickString(dst.Token, f.Engine.Token)
	dst.RoomID = pickString(dst.RoomID, f.Room.ID)
	dst.KeyHex = pickString(dst.KeyHex, f.Crypto.Key)
	dst.SOCKSHost = pickString(dst.SOCKSHost, f.SOCKS.Host)
	dst.SOCKSPort = pickInt(dst.SOCKSPort, f.SOCKS.Port)
	dst.SOCKSUser = pickString(dst.SOCKSUser, f.SOCKS.User)
	dst.SOCKSPass = pickString(dst.SOCKSPass, f.SOCKS.Pass)
	dst.DNSServer = pickString(dst.DNSServer, f.Net.DNS)
	dst.SOCKSProxyAddr = pickString(dst.SOCKSProxyAddr, f.SOCKS.ProxyAddr)
	dst.SOCKSProxyPort = pickInt(dst.SOCKSProxyPort, f.SOCKS.ProxyPort)
	dst.VideoWidth = pickInt(dst.VideoWidth, f.Video.Width)
	dst.VideoHeight = pickInt(dst.VideoHeight, f.Video.Height)
	dst.VideoFPS = pickInt(dst.VideoFPS, f.Video.FPS)
	dst.VideoBitrate = pickString(dst.VideoBitrate, f.Video.Bitrate)
	dst.VideoHW = pickString(dst.VideoHW, f.Video.HW)
	dst.VideoQRSize = pickInt(dst.VideoQRSize, f.Video.QRSize)
	dst.VideoQRRecovery = pickString(dst.VideoQRRecovery, f.Video.QRRecovery)
	dst.VideoCodec = pickString(dst.VideoCodec, f.Video.Codec)
	dst.VideoTileModule = pickInt(dst.VideoTileModule, f.Video.TileModule)
	dst.VideoTileRS = pickInt(dst.VideoTileRS, f.Video.TileRS)
	dst.VP8FPS = pickInt(dst.VP8FPS, f.VP8.FPS)
	dst.VP8BatchSize = pickInt(dst.VP8BatchSize, f.VP8.BatchSize)
	dst.SEIFPS = pickInt(dst.SEIFPS, f.SEI.FPS)
	dst.SEIBatchSize = pickInt(dst.SEIBatchSize, f.SEI.BatchSize)
	dst.SEIFragmentSize = pickInt(dst.SEIFragmentSize, f.SEI.FragmentSize)
	dst.SEIAckTimeoutMS = pickInt(dst.SEIAckTimeoutMS, f.SEI.AckTimeoutMS)
	dst.Amount = pickInt(dst.Amount, f.Gen.Amount)
	dst.SOCKSProxyUser = pickString(dst.SOCKSProxyUser, f.SOCKS.ProxyUser)
	dst.SOCKSProxyPass = pickString(dst.SOCKSProxyPass, f.SOCKS.ProxyPass)
	dst.WarpProxyAddr = pickString(dst.WarpProxyAddr, f.Warp.ProxyAddr)
	dst.WarpProxyPort = pickInt(dst.WarpProxyPort, f.Warp.ProxyPort)
	dst.SubEnabled = f.Subscription.Enabled
	dst.SubPort = pickInt(dst.SubPort, f.Subscription.Port)
	dst.SubDBPath = pickString(dst.SubDBPath, f.Subscription.DBPath)
	dst.SubAPIToken = pickString(dst.SubAPIToken, f.Subscription.APIToken)
	return dst
}

func pickString(cli, yamlVal string) string {
	if cli != "" {
		return cli
	}
	return yamlVal
}

func pickInt(cli, yamlVal int) int {
	if cli != 0 {
		return cli
	}
	return yamlVal
}
