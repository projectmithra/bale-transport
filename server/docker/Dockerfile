FROM node:20-alpine

# Install tini for proper PID-1 signal handling
RUN apk add --no-cache tini

WORKDIR /app

# Install dependencies first for Docker layer caching
COPY server/unwrapper/package.json .
RUN npm install --omit=dev --no-audit --no-fund && \
    npm cache clean --force

COPY server/unwrapper/unwrapper.js .

ENV NODE_ENV=production
ENV PORT=80
ENV BACKEND=ws://127.0.0.1:8443
ENV BACKEND_PATH=/api/v4/sync/data-stream

# Run as the unprivileged `node` user that the base image provides
USER node

EXPOSE 80

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["node", "unwrapper.js"]
