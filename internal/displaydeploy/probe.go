package displaydeploy

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/galendai/homepi-mon/internal/displayconfig"
)

//go:embed probe.sh
var probeScript string

type StartupStatus struct {
	displayconfig.PageSettings
	Flags    []string `json:"flags,omitempty"`
	Renderer string   `json:"renderer,omitempty"`
}

func parseProbe(raw []byte) (RemoteStatus, error) {
	var s RemoteStatus
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "drop_in" {
			s.DropIns = append(s.DropIns, value)
		} else {
			values[key] = value
		}
	}
	s.ServiceActive = values["active"] == "active"
	var err error
	s.Restarts, err = strconv.Atoi(values["restarts"])
	if err != nil || s.Restarts < 0 || values["active"] == "" || values["binary"] == "" || values["file_style"] == "" {
		return RemoteStatus{}, errors.New("display deploy: incomplete remote status")
	}
	s.BinaryVersion, s.DeviceID, s.NodeURLHash = values["binary"], values["device_id"], values["node_url_hash"]
	hash, hashErr := hex.DecodeString(s.NodeURLHash)
	if hashErr != nil || len(hash) != sha256.Size || s.DeviceID == "" {
		return RemoteStatus{}, errors.New("display deploy: incomplete remote device binding")
	}
	s.fileDigest = values["file_digest"]
	digest, err := hex.DecodeString(s.fileDigest)
	if err != nil || len(digest) != sha256.Size {
		return RemoteStatus{}, errors.New("display deploy: unverified remote environment file")
	}
	s.SnapshotEpoch, s.SnapshotTime = values["epoch"], values["generated"]
	s.SnapshotVersion, _ = strconv.ParseUint(values["version"], 10, 64)
	s.Connected = s.ServiceActive && s.SnapshotVersion > 0 && s.SnapshotEpoch != ""
	s.SupportsPageEnv = values["rotation_env"] == "yes"
	settings := func(prefix string) displayconfig.PageSettings {
		return displayconfig.PageSettings{
			Style: values[prefix+"style"], PageOrder: values[prefix+"page-order"], PageDwellSeconds: values[prefix+"page-dwell-seconds"],
			HomeOnly: values[prefix+"home-only"], PageSubset: values[prefix+"pages"], PageLock: values[prefix+"page-only"],
		}
	}
	s.PageSettings, _, err = settings("file_").Resolve()
	if err != nil {
		return RemoteStatus{}, errors.New("display deploy: invalid settings in the remote environment file")
	}
	s.Verified = true
	if values["process_verified"] == "yes" {
		flags := make(map[string]string)
		var labels []string
		for _, name := range []string{"style", "page-order", "page-dwell-seconds", "home-only", "pages", "page-only"} {
			if value, ok := values["flag_"+name]; ok {
				flags[name] = value
				labels = append(labels, "-"+name+"="+value)
			}
		}
		process, _, err := settings("process_").WithFlags(flags).Resolve()
		renderer := values["renderer"]
		// An old or replaced executable may ignore the new environment keys.
		// Its explicit page flags can still establish the effective mode.
		_, pagesFlag := flags["pages"]
		_, lockFlag := flags["page-only"]
		usesPageEnv := process.PageSubset != "" && !pagesFlag || process.PageLock != "" && !lockFlag
		if err == nil && (!usesPageEnv || renderer == "gui" || renderer == "terminal") {
			sort.Strings(labels)
			s.Startup = &StartupStatus{PageSettings: process, Flags: labels, Renderer: renderer}
		}
	}
	s.StartupState = s.startupStep()
	return s, nil
}
