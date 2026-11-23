//go:build darwin && cgo

package connector

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

static CFMutableDictionaryRef account_query(const char *service, const char *account) {
	CFStringRef s = CFStringCreateWithCString(NULL, service, kCFStringEncodingUTF8);
	CFStringRef a = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
	CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(query, kSecAttrService, s);
	CFDictionarySetValue(query, kSecAttrAccount, a);
	CFRelease(s);
	CFRelease(a);
	return query;
}

static OSStatus put_credential(const char *service, const char *account, const char *secret) {
	CFMutableDictionaryRef query = account_query(service, account);
	CFDataRef data = CFDataCreate(NULL, (const UInt8 *)secret, strlen(secret));
	const void *keys[] = {kSecValueData};
	const void *values[] = {data};
	CFDictionaryRef update = CFDictionaryCreate(NULL, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	OSStatus status = SecItemUpdate(query, update);
	if (status == errSecItemNotFound) {
		CFDictionarySetValue(query, kSecValueData, data);
		status = SecItemAdd(query, NULL);
	}
	CFRelease(update);
	CFRelease(data);
	CFRelease(query);
	return status;
}

static OSStatus get_credential(const char *service, const char *account, CFDataRef *data) {
	CFMutableDictionaryRef query = account_query(service, account);
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	OSStatus status = SecItemCopyMatching(query, (CFTypeRef *)data);
	CFRelease(query);
	return status;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func readCredential(namespace, account string) ([]byte, error) {
	service, ref := C.CString(namespace), C.CString(account)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(ref))
	var data C.CFDataRef
	switch C.get_credential(service, ref, &data) {
	case C.errSecSuccess:
		defer C.CFRelease(C.CFTypeRef(data))
		return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(data)), C.int(C.CFDataGetLength(data))), nil
	case C.errSecItemNotFound:
		return nil, errNoCredential
	default:
		return nil, errors.New("GitHub Keychain read unavailable")
	}
}

func writeCredential(namespace, account string, payload []byte) error {
	service, ref, secret := C.CString(namespace), C.CString(account), C.CString(string(payload))
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(ref))
	defer C.free(unsafe.Pointer(secret))
	if C.put_credential(service, ref, secret) != C.errSecSuccess {
		return errors.New("GitHub Keychain write unavailable")
	}
	return nil
}
