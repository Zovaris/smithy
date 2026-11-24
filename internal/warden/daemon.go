package warden

import (
	"os"
	"strconv"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/daemon"
)

func daemonDoctor(r *report) {
	i, err := daemon.ReadInfo()
	switch {
	case err != nil:
		r.warn("daemon: "+err.Error(), "remove "+daemon.InfoFile()+" if no daemon runs")
	case i == nil:
	case daemon.CheckLoopback(i.Addr) != nil:
		r.bad("daemon: listening beyond loopback at "+i.Addr, "stop it (kill "+strconv.Itoa(i.PID)+"); smithy daemon only binds 127.0.0.1")
	case !auto.AliveAs(i.PID, i.PIDStart):
		r.warn("daemon: stale "+daemon.InfoFile()+" (pid "+strconv.Itoa(i.PID)+" is gone)", "start it again (smithy daemon --bg) or remove the file")
	default:
		r.ok("daemon: running (pid " + strconv.Itoa(i.PID) + ", http://" + i.Addr + ")")
	}
	if _, err := os.Stat(daemon.TokenFile()); err == nil {
		if _, err := daemon.LoadToken(daemon.TokenFile(), false); err != nil {
			r.bad("daemon token: "+err.Error(), "chmod 600 "+daemon.TokenFile())
		}
	}
}
