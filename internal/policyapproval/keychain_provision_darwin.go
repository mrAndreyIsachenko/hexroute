//go:build darwin && cgo

package policyapproval

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreFoundation -framework Foundation -framework LocalAuthentication -framework Security

#include <CoreFoundation/CoreFoundation.h>
#include <LocalAuthentication/LocalAuthentication.h>
#include <Security/Security.h>
#include <stdlib.h>

static void hexroute_secure_free(void *value, size_t length) {
    if (value == NULL) return;
    volatile unsigned char *cursor = (volatile unsigned char *)value;
    while (length-- > 0) {
        *cursor++ = 0;
    }
    free(value);
}

static OSStatus hexroute_add_user_presence_secret(
    const char *service,
    const char *account,
    const unsigned char *value,
    CFIndex value_length
) {
    OSStatus status = errSecParam;
    CFErrorRef access_error = NULL;
    CFStringRef service_value = NULL;
    CFStringRef account_value = NULL;
    CFStringRef description_value = NULL;
    CFDataRef secret_value = NULL;
    SecAccessControlRef access = NULL;
    CFDictionaryRef attributes = NULL;

    service_value = CFStringCreateWithCString(
        kCFAllocatorDefault, service, kCFStringEncodingUTF8
    );
    account_value = CFStringCreateWithCString(
        kCFAllocatorDefault, account, kCFStringEncodingUTF8
    );
    description_value = CFStringCreateWithCString(
        kCFAllocatorDefault,
        "Hexroute policy signing key",
        kCFStringEncodingUTF8
    );
    secret_value = CFDataCreateWithBytesNoCopy(
        kCFAllocatorDefault,
        value,
        value_length,
        kCFAllocatorNull
    );
    access = SecAccessControlCreateWithFlags(
        kCFAllocatorDefault,
        kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly,
        kSecAccessControlUserPresence,
        &access_error
    );
    if (service_value == NULL || account_value == NULL ||
        description_value == NULL || secret_value == NULL || access == NULL) {
        goto cleanup;
    }

    const void *keys[] = {
        kSecClass,
        kSecAttrService,
        kSecAttrAccount,
        kSecAttrDescription,
        kSecValueData,
        kSecAttrAccessControl,
        kSecUseDataProtectionKeychain,
    };
    const void *values[] = {
        kSecClassGenericPassword,
        service_value,
        account_value,
        description_value,
        secret_value,
        access,
        kCFBooleanTrue,
    };
    attributes = CFDictionaryCreate(
        kCFAllocatorDefault,
        keys,
        values,
        sizeof(keys) / sizeof(keys[0]),
        &kCFTypeDictionaryKeyCallBacks,
        &kCFTypeDictionaryValueCallBacks
    );
    if (attributes == NULL) {
        goto cleanup;
    }
    status = SecItemAdd(attributes, NULL);

cleanup:
    if (attributes != NULL) CFRelease(attributes);
    if (access != NULL) CFRelease(access);
    if (access_error != NULL) CFRelease(access_error);
    if (secret_value != NULL) CFRelease(secret_value);
    if (description_value != NULL) CFRelease(description_value);
    if (account_value != NULL) CFRelease(account_value);
    if (service_value != NULL) CFRelease(service_value);
    return status;
}

static OSStatus hexroute_copy_user_presence_secret(
    const char *service,
    const char *account,
    unsigned char **output,
    CFIndex *output_length
) {
    OSStatus status = errSecParam;
    CFStringRef service_value = NULL;
    CFStringRef account_value = NULL;
    LAContext *auth_context = nil;
    CFDictionaryRef query = NULL;
    CFTypeRef result = NULL;
    unsigned char *copy = NULL;

    *output = NULL;
    *output_length = 0;
    service_value = CFStringCreateWithCString(
        kCFAllocatorDefault, service, kCFStringEncodingUTF8
    );
    account_value = CFStringCreateWithCString(
        kCFAllocatorDefault, account, kCFStringEncodingUTF8
    );
    auth_context = [[LAContext alloc] init];
    auth_context.localizedReason = @"Approve Hexroute policy signature";
    if (service_value == NULL || account_value == NULL || auth_context == nil) {
        goto cleanup;
    }

    const void *keys[] = {
        kSecClass,
        kSecAttrService,
        kSecAttrAccount,
        kSecReturnData,
        kSecMatchLimit,
        kSecUseAuthenticationContext,
        kSecUseDataProtectionKeychain,
    };
    const void *values[] = {
        kSecClassGenericPassword,
        service_value,
        account_value,
        kCFBooleanTrue,
        kSecMatchLimitOne,
        auth_context,
        kCFBooleanTrue,
    };
    query = CFDictionaryCreate(
        kCFAllocatorDefault,
        keys,
        values,
        sizeof(keys) / sizeof(keys[0]),
        &kCFTypeDictionaryKeyCallBacks,
        &kCFTypeDictionaryValueCallBacks
    );
    if (query == NULL) {
        goto cleanup;
    }
    status = SecItemCopyMatching(query, &result);
    if (status != errSecSuccess) {
        goto cleanup;
    }
    if (result == NULL || CFGetTypeID(result) != CFDataGetTypeID()) {
        status = errSecDecode;
        goto cleanup;
    }
    CFIndex length = CFDataGetLength((CFDataRef)result);
    if (length <= 0 || length > 256) {
        status = errSecDecode;
        goto cleanup;
    }
    copy = malloc((size_t)length);
    if (copy == NULL) {
        status = errSecAllocate;
        goto cleanup;
    }
    CFDataGetBytes((CFDataRef)result, CFRangeMake(0, length), copy);
    *output = copy;
    *output_length = length;
    copy = NULL;

cleanup:
    if (copy != NULL) free(copy);
    if (result != NULL) CFRelease(result);
    if (query != NULL) CFRelease(query);
    if (auth_context != nil) [auth_context release];
    if (account_value != NULL) CFRelease(account_value);
    if (service_value != NULL) CFRelease(service_value);
    return status;
}
*/
import "C"

import (
	"context"
	"unsafe"
)

type UserPresenceKeychainStore struct{}

func (UserPresenceKeychainStore) StoreUserPresence(
	ctx context.Context,
	service string,
	account string,
	value []byte,
) error {
	if ctx == nil || !keychainIdentifier.MatchString(service) ||
		!keychainIdentifier.MatchString(account) || len(value) == 0 || len(value) > maxKeychainSeed {
		return ErrInvalidKeychainConfig
	}
	select {
	case <-ctx.Done():
		return ErrKeychainAccess
	default:
	}
	cService := C.CString(service)
	cAccount := C.CString(account)
	cValue := C.CBytes(value)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))
	defer C.hexroute_secure_free(cValue, C.size_t(len(value)))
	status := C.hexroute_add_user_presence_secret(
		cService,
		cAccount,
		(*C.uchar)(cValue),
		C.CFIndex(len(value)),
	)
	return keychainStatus(status)
}

func (UserPresenceKeychainStore) ReadUserPresence(
	ctx context.Context,
	service string,
	account string,
) ([]byte, error) {
	if ctx == nil || !keychainIdentifier.MatchString(service) ||
		!keychainIdentifier.MatchString(account) {
		return nil, ErrInvalidKeychainConfig
	}
	type readResult struct {
		content []byte
		err     error
	}
	result := make(chan readResult)
	go func() {
		content, err := copyUserPresenceSecret(service, account)
		select {
		case result <- readResult{content: content, err: err}:
		case <-ctx.Done():
			clear(content)
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ErrKeychainInteractionDenied
	case read := <-result:
		return read.content, read.err
	}
}

func copyUserPresenceSecret(service, account string) ([]byte, error) {
	cService := C.CString(service)
	cAccount := C.CString(account)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))
	var output *C.uchar
	var outputLength C.CFIndex
	status := C.hexroute_copy_user_presence_secret(
		cService,
		cAccount,
		&output,
		&outputLength,
	)
	if output != nil {
		defer C.hexroute_secure_free(unsafe.Pointer(output), C.size_t(outputLength))
	}
	if err := keychainStatus(status); err != nil {
		return nil, err
	}
	if output == nil || outputLength <= 0 || outputLength > maxKeychainSeed {
		return nil, ErrKeychainAccess
	}
	return C.GoBytes(unsafe.Pointer(output), C.int(outputLength)), nil
}

func keychainStatus(status C.OSStatus) error {
	switch status {
	case C.errSecSuccess:
		return nil
	case C.errSecDuplicateItem:
		return ErrKeychainDuplicate
	case C.errSecAuthFailed, C.errSecInteractionNotAllowed, C.errSecUserCanceled:
		return ErrKeychainInteractionDenied
	case C.errSecMissingEntitlement:
		return ErrKeychainMissingEntitlement
	case C.errSecParam, C.errSecNotAvailable, C.errSecUnimplemented:
		return ErrKeychainAccessControl
	default:
		return ErrKeychainAccess
	}
}

var _ KeychainProvisioner = UserPresenceKeychainStore{}
var _ KeychainReader = UserPresenceKeychainStore{}
