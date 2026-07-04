FROM ubuntu:22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    openssh-server \
    git \
    curl \
    wget \
    vim \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /run/sshd

COPY entrypoint.sh /usr/local/bin/devbox-entrypoint.sh
RUN chmod +x /usr/local/bin/devbox-entrypoint.sh

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/devbox-entrypoint.sh"]
