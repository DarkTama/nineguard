FROM node:24-slim

ARG BILI_VERSION=latest

RUN npm install -g billion-context@${BILI_VERSION}

ENV ACP_AUTO_UPDATE=0

EXPOSE 8787

VOLUME ["/root/.config/billion-context", "/root/.local/share/billion-context", "/root/.cache/billion-context", "/root/.local/state/billion-context"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:8787/__bili/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

CMD ["bili", "--host", "0.0.0.0", "--port", "8787"]
