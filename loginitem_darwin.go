//go:build darwin && tray

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>

// The status values, flattened for Go. SMAppService is the supported way to open at
// login since macOS 13: it registers the app bundle itself, with no helper tool and
// no deprecated shared file list.
enum {
    relayLoginUnknown = 0,
    relayLoginEnabled = 1,
    relayLoginNotRegistered = 2,
    relayLoginRequiresApproval = 3,
};

static int relayLoginStatus(void) {
    @autoreleasepool {
        switch ([SMAppService mainAppService].status) {
        case SMAppServiceStatusEnabled:
            return relayLoginEnabled;
        case SMAppServiceStatusRequiresApproval:
            return relayLoginRequiresApproval;
        case SMAppServiceStatusNotRegistered:
        case SMAppServiceStatusNotFound:
            return relayLoginNotRegistered;
        default:
            return relayLoginUnknown;
        }
    }
}

// Returns an autoreleased description of the failure, or NULL on success. macOS
// explains these better than we could guess ("Operation not permitted" when the app
// is quarantined, for instance), so the message is passed through.
static const char *relaySetLogin(int enable) {
    @autoreleasepool {
        SMAppService *service = [SMAppService mainAppService];
        NSError *error = nil;
        BOOL ok = enable ? [service registerAndReturnError:&error]
                         : [service unregisterAndReturnError:&error];

        if (ok) {
            return NULL;
        }

        // Turning off something that was never on is the state the caller wanted.
        if (!enable && service.status == SMAppServiceStatusNotRegistered) {
            return NULL;
        }

        NSString *message = error.localizedDescription ?: @"macOS refused the change";

        return [message UTF8String];
    }
}
*/
import "C"

import (
	"errors"
	"os"
	"strings"
)

// bundlePath returns the .app this binary is running from, or "" when it is not in
// one. SMAppService registers a bundle, so a bare `go build -tags tray` binary run
// from a terminal has nothing to register.
func bundlePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	const inside = ".app/Contents/MacOS/"

	i := strings.LastIndex(exe, inside)
	if i < 0 {
		return ""
	}

	return exe[:i+len(".app")]
}

func loginItemStatus() (loginItem, error) {
	path := bundlePath()
	if path == "" {
		return loginItem{}, nil
	}

	// A login item records where the app is. Registering one that lives on a
	// mounted disk image would point at a path that disappears when it is ejected,
	// so the control is offered only once the app has been copied out of it.
	if strings.HasPrefix(path, "/Volumes/") {
		return loginItem{}, nil
	}

	switch C.relayLoginStatus() {
	case C.relayLoginEnabled:
		return loginItem{Supported: true, Enabled: true}, nil
	case C.relayLoginRequiresApproval:
		return loginItem{Supported: true, NeedsApproval: true}, nil
	default:
		return loginItem{Supported: true}, nil
	}
}

func setLoginItem(on bool) error {
	status, _ := loginItemStatus()
	if !status.Supported {
		return errors.New("move PageCrawl Relay to your Applications folder first, then try again")
	}

	enable := C.int(0)
	if on {
		enable = C.int(1)
	}

	if msg := C.relaySetLogin(enable); msg != nil {
		return errors.New(C.GoString(msg))
	}

	return nil
}
