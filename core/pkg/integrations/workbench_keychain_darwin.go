//go:build darwin && cgo

package integrations

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>


static CFDictionaryRef keychain_query(const char *service, const char *account) {
	CFStringRef serviceString = CFStringCreateWithCString(NULL, service, kCFStringEncodingUTF8);
	CFStringRef accountString = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
	const void *keys[] = {kSecClass, kSecAttrService, kSecAttrAccount};
	const void *values[] = {kSecClassGenericPassword, serviceString, accountString};
	CFDictionaryRef query = CFDictionaryCreate(NULL, keys, values, 3, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFRelease(serviceString);
	CFRelease(accountString);
	return query;
}
static OSStatus keychain_put(const char *service, const char *account, const char *secret) {
	CFDictionaryRef base = keychain_query(service, account);
	CFMutableDictionaryRef query = CFDictionaryCreateMutableCopy(NULL, 0, base);
	CFDataRef data = CFDataCreate(NULL, (const UInt8 *)secret, (CFIndex)strlen(secret));
	CFDictionarySetValue(query, kSecValueData, data);
	OSStatus status = SecItemAdd(query, NULL);
	CFRelease(data);
	CFRelease(query);
	CFRelease(base);
	return status;
}
static OSStatus keychain_replace(const char *service, const char *account, const char *secret) {
	CFDictionaryRef query = keychain_query(service, account);
	CFDataRef data = CFDataCreate(NULL, (const UInt8 *)secret, (CFIndex)strlen(secret));
	const void *keys[] = {kSecValueData};
	const void *values[] = {data};
	CFDictionaryRef updates = CFDictionaryCreate(NULL, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	OSStatus status = SecItemUpdate(query, updates);
	CFRelease(updates);
	CFRelease(data);
	CFRelease(query);
	return status;
}
static OSStatus keychain_get(const char *service, const char *account, CFDataRef *data) {
	CFDictionaryRef base = keychain_query(service, account);
	CFMutableDictionaryRef query = CFDictionaryCreateMutableCopy(NULL, 0, base);
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(query, &result);
	*data = (CFDataRef)result;
	CFRelease(query);
	CFRelease(base);
	return status;
}
static OSStatus keychain_delete(const char *service, const char *account) {
	CFDictionaryRef query = keychain_query(service, account);
	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	return status;
}
static CFArrayRef keychain_list(const char *service, OSStatus *status) {
	CFStringRef serviceString = CFStringCreateWithCString(NULL, service, kCFStringEncodingUTF8);
	const void *keys[] = {kSecClass, kSecAttrService, kSecReturnAttributes, kSecMatchLimit};
	const void *values[] = {kSecClassGenericPassword, serviceString, kCFBooleanTrue, kSecMatchLimitAll};
	CFDictionaryRef query = CFDictionaryCreate(NULL, keys, values, 4, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFTypeRef result = NULL;
	*status = SecItemCopyMatching(query, &result);
	CFRelease(query);
	CFRelease(serviceString);
	return (CFArrayRef)result;
}
static CFStringRef keychain_account(CFArrayRef items, CFIndex index) {
	CFDictionaryRef item = (CFDictionaryRef)CFArrayGetValueAtIndex(items, index);
	return (CFStringRef)CFDictionaryGetValue(item, kSecAttrAccount);
}
*/
import "C"

import (
	"context"
	"unsafe"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchKeychain struct {
	service string
}

func NewWorkbenchKeychain(namespace string) WorkbenchCredentialStore {
	return workbenchKeychain{service: namespace}
}

func keychainError(status C.OSStatus) error {
	switch status {
	case C.errSecSuccess:
		return nil
	case C.errSecItemNotFound:
		return common.Err("NOT_FOUND", "Credential was not found")
	case C.errSecDuplicateItem:
		return common.Err("CONFLICT", "Credential already exists")
	case C.errSecInteractionNotAllowed, C.errSecAuthFailed:
		return common.Err("UNAUTHORIZED", "Keychain is locked or access was denied")
	default:
		return common.Err("SOURCE_UNAVAILABLE", "Keychain operation failed")
	}
}

func (k workbenchKeychain) Put(ctx context.Context, ref, secret string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if k.service == "" || ref == "" || secret == "" {
		return common.Err("INVALID_ARGUMENT", "Credential reference and value are required")
	}
	service, account, value := C.CString(k.service), C.CString(ref), C.CString(secret)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	defer C.free(unsafe.Pointer(value))
	return keychainError(C.keychain_put(service, account, value))
}

func (k workbenchKeychain) Replace(ctx context.Context, ref, secret string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if k.service == "" || ref == "" || secret == "" {
		return common.Err("INVALID_ARGUMENT", "Credential reference and value are required")
	}
	service, account, value := C.CString(k.service), C.CString(ref), C.CString(secret)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	defer C.free(unsafe.Pointer(value))
	return keychainError(C.keychain_replace(service, account, value))
}

func (k workbenchKeychain) Get(ctx context.Context, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	service, account := C.CString(k.service), C.CString(ref)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	var data C.CFDataRef
	status := C.keychain_get(service, account, &data)
	if err := keychainError(status); err != nil {
		return "", err
	}
	defer C.CFRelease(C.CFTypeRef(data))
	length := C.CFDataGetLength(data)
	return C.GoStringN((*C.char)(unsafe.Pointer(C.CFDataGetBytePtr(data))), C.int(length)), nil
}

func (k workbenchKeychain) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	service, account := C.CString(k.service), C.CString(ref)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	status := C.keychain_delete(service, account)
	if status == C.errSecItemNotFound {
		return nil
	}
	return keychainError(status)
}

func (k workbenchKeychain) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service := C.CString(k.service)
	defer C.free(unsafe.Pointer(service))
	var status C.OSStatus
	items := C.keychain_list(service, &status)
	if status == C.errSecItemNotFound {
		return []string{}, nil
	}
	if err := keychainError(status); err != nil {
		return nil, err
	}
	defer C.CFRelease(C.CFTypeRef(items))
	var refs []string
	for i := C.CFIndex(0); i < C.CFArrayGetCount(items); i++ {
		account := C.keychain_account(items, i)
		if account == 0 {
			continue
		}
		size := C.CFStringGetMaximumSizeForEncoding(C.CFStringGetLength(account), C.kCFStringEncodingUTF8) + 1
		buffer := make([]byte, int(size))
		if C.CFStringGetCString(account, (*C.char)(unsafe.Pointer(&buffer[0])), size, C.kCFStringEncodingUTF8) != 0 {
			refs = append(refs, C.GoString((*C.char)(unsafe.Pointer(&buffer[0]))))
		}
	}
	return refs, nil
}
