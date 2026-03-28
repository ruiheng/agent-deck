FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /root

RUN curl -fsSL https://opencode.ai/install | bash
ENV PATH="/root/.opencode/bin:${PATH}"

RUN mkdir -p \
    /root/.config/opencode \
    /root/.local/share/opencode \
    /root/.cache/opencode \
    /root/.agent-deck/hooks

RUN chmod 755 /root

WORKDIR /workspace

CMD ["sleep", "infinity"]
