# The fnOS gateway bridge: a Unix-socket front for magpie web's one-port
# server. Built for the FPK's compose file; multi-arch via buildx in CI.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bridge .

FROM scratch
COPY --from=build /out/bridge /bridge
# Runs as root so the socket file can be created in the host-owned install
# target directory mounted at /sock; unauthenticated socket clients only
# reach magpie's own sign-in page (the web key cookie is injected for
# fnOS-authenticated sessions alone).
ENTRYPOINT ["/bridge"]
