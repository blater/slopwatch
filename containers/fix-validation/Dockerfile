ARG BASE_IMAGE
FROM ${BASE_IMAGE}

LABEL com.slopwatch.confinement.protocol="v1"

# Built by `make build-fix-validation-image`; Docker selects the explicit
# non-root uid:gid at runtime.
COPY --chmod=0555 build/fix-validation/slopmark /usr/local/bin/slopmark
