# Secret Canary Fixtures

Every value in `canaries.json` is synthetic and intentionally invalid as a
production credential. Serializer, logger, incident-bundle and diagnostics
tests load these sentinels and fail if any appears in emitted output.

The canaries cover Pritunl PIN, TOTP seed, generated OTP, VLESS credential,
Reality private key and MTG secret classes.
