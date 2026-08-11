#import <UserNotifications/UserNotifications.h>

void request_notification_authorization(void (*callback)(int granted)) {
    // +currentNotificationCenter raises NSInternalInconsistencyException
    // ("bundleProxyForCurrentProcess is nil") when the executable is not inside
    // an .app bundle — which is exactly how the debug binary runs (`make dev`,
    // `make dev-signed`). An uncaught ObjC exception aborts the process, so
    // without this guard the app cannot start outside a bundle at all.
    if ([[NSBundle mainBundle] bundleIdentifier] == nil) {
        if (callback) {
            callback(0);
        }
        return;
    }

    UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
    UNAuthorizationOptions options = UNAuthorizationOptionAlert | UNAuthorizationOptionSound | UNAuthorizationOptionBadge;

    [center requestAuthorizationWithOptions:options
                          completionHandler:^(BOOL granted, NSError * _Nullable error) {
        if (callback) {
            callback(granted ? 1 : 0);
        }
    }];
}
