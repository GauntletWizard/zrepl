package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/util/envconst"
	"github.com/zrepl/zrepl/internal/zfs/zfscmd"
)

var encryptionCLISupport struct {
	once      sync.Once
	supported bool
	err       error
}

func EncryptionCLISupported(ctx context.Context) (bool, error) {
	encryptionCLISupport.once.Do(func() {
		// "feature discovery"
		cmd := zfscmd.CommandContext(ctx, "zfs", "load-key")
		output, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ok && !ee.Exited() {
			encryptionCLISupport.err = errors.Wrap(err, "native encryption cli support feature check failed")
		}
		def := strings.Contains(string(output), "load-key") && strings.Contains(string(output), "keylocation")
		encryptionCLISupport.supported = envconst.Bool("ZREPL_EXPERIMENTAL_ZFS_ENCRYPTION_CLI_SUPPORTED", def)
		debug("encryption cli feature check complete %#v", &encryptionCLISupport)
	})
	return encryptionCLISupport.supported, encryptionCLISupport.err
}

// returns false, nil if encryption is not supported
func ZFSGetEncryptionEnabled(ctx context.Context, fs string) (enabled bool, err error) {
	defer func(e *error) {
		if *e != nil {
			*e = fmt.Errorf("zfs get encryption enabled fs=%q: %s", fs, *e)
		}
	}(&err)
	if supp, err := EncryptionCLISupported(ctx); err != nil {
		return false, err
	} else if !supp {
		return false, nil
	}

	if err := validateZFSFilesystem(fs); err != nil {
		return false, err
	}

	props, err := zfsGet(ctx, fs, []string{"encryption"}, SourceAny)
	if err != nil {
		return false, errors.Wrap(err, "cannot get `encryption` property")
	}
	val := props.Get("encryption")
	switch val {
	case "":
		panic("zfs get should return a value for `encryption`")
	case "-":
		return false, errors.New("`encryption` property should never be \"-\"")
	case "off":
		return false, nil
	default:
		// we don't want to hardcode the cipher list, and we checked for != 'off'
		// ==> assume any other value means encryption is enabled
		// TODO add test to OpenZFS test suite
		return true, nil
	}
}

/*
returns true if the key is unloaded. This does not matter for raw sends, but keys need to be loaded for live sends.

This mildly short-circuits sends that were about to fail, but the real reason for this is a zfs recieve bug that can corrupt volumes that are sent-to with the dataset unloaded.
https://github.com/openzfs/zfs/issues/14055
*/
func ZFSGetKeyUnloaded(ctx context.Context, fs string) (loaded bool, err error) {
	defer func(e *error) {
		if *e != nil {
			*e = fmt.Errorf("zfs get key loaded fs=%q: %s", fs, *e)
		}
	}(&err)
	if supp, err := EncryptionCLISupported(ctx); err != nil {
		return false, err
	} else if !supp {
		return false, nil
	}

	props, err := zfsGet(ctx, fs, []string{"keystatus"}, SourceAny)
	if err != nil {
		return false, errors.Wrap(err, "cannot get `keystatus` property")
	}
	val := props.Get("keystatus")
	switch val {
	case "":
		panic("zfs get should return a value for `keystatus`")
	case "available":
		return false, nil
	case "unavailable":
		return true, nil
	default:
		panic("Unknown key status")
	}
}
