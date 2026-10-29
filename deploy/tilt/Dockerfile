# The image `tilt up` builds: the release image's runtime (the Dockerfile at
# the repository root, stage 3) around a binary and a console built on the
# host, so a change rebuilds in seconds instead of from scratch. The context
# is .tilt/image, which the Tiltfile fills: humagen (linux, static) and web/.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S -g 10001 humagen \
 && adduser -S -D -H -u 10001 -G humagen -h /var/lib/warp humagen \
 && mkdir -p /var/lib/warp /etc/warp \
 && chown humagen:humagen /var/lib/warp
COPY humagen /usr/local/bin/humagen
COPY web /usr/share/warp/web
ENV HOME=/var/lib/warp \
    XDG_CACHE_HOME=/var/lib/warp/cache
WORKDIR /var/lib/warp
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/humagen"]
CMD ["serve", "-f", "/etc/warp", "-addr", "127.0.0.1:8080", "-web", "/usr/share/warp/web"]
