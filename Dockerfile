FROM gcr.io/distroless/static
ARG TARGETPLATFORM
VOLUME "/data"
ENV PAPRIKA_DATA_ROOT /data
COPY $TARGETPLATFORM/paprika /usr/bin/paprika
ENTRYPOINT ["/usr/bin/paprika"]
