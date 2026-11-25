package warden

import (
	"errors"
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
		f, pid, start := daemon.InfoFile(), i.PID, i.PIDStart
		r.offer("remove stale "+f, func() error {
			if now, err := daemon.ReadInfo(); err == nil && now != nil && (now.PID != pid || now.PIDStart != start) {
				return errors.New("a daemon wrote it again; left it")
			}
			return os.Remove(f)
		})
		r.warn("daemon: stale "+daemon.InfoFile()+" (pid "+strconv.Itoa(i.PID)+" is gone)", "start it again (smithy daemon --bg) or remove the file")
	default:
		r.ok("daemon: running (pid " + strconv.Itoa(i.PID) + ", http://" + i.Addr + ")")
	}
	if _, err := os.Stat(daemon.TokenFile()); err == nil {
		if _, err := daemon.LoadToken(daemon.TokenFile(), false); err != nil {
			f := daemon.TokenFile()
			if fi, err := os.Lstat(f); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o077 != 0 {
				r.offer("chmod 600 "+f, func() error { return os.Chmod(f, 0o600) })
			}
			r.bad("daemon token: "+err.Error(), "chmod 600 "+daemon.TokenFile())
		}
	}
}
