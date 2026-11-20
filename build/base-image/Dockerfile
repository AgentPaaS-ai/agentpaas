# py311-r1 base image.
# The runtime base is the pinned distroless image packed images already use.
# The dirs stage only creates an empty /app and a writable lock file; it is
# not the runtime base. /agentpaas stays root-owned so the harness cannot be
# replaced. The lock file is created first by the harness copy, then replaced
# with a 64000-owned file so unpack can overwrite it.
FROM python:3.11-slim@sha256:07d4824036af339fa1b6ef769cc99d9c10a141769538b52b3b597c84f7a7b535 AS dirs
RUN mkdir -p /empty/app /empty/agentpaas \
 && touch /empty/agentpaas/requirements.lock \
 && chown 64000:64000 /empty/app /empty/agentpaas/requirements.lock \
 && chmod 755 /empty/app \
 && chmod 644 /empty/agentpaas/requirements.lock

FROM gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV AGENTPAAS_ADDR=0.0.0.0:8080
ENV AGENTPAAS_ALLOW_NON_LOOPBACK_LISTEN=1
ENV PYTHONPATH=/app/deps
COPY --chown=0:0 --chmod=555 harness /agentpaas/harness
COPY --from=dirs --chown=64000:64000 /empty/agentpaas/requirements.lock /agentpaas/requirements.lock
COPY --from=dirs --chown=64000:64000 /empty/app /app
WORKDIR /app
USER 64000:64000
ENTRYPOINT ["/agentpaas/harness"]
