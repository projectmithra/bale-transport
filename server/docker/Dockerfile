FROM node:20-alpine

WORKDIR /app
COPY server/unwrapper/package.json .
RUN npm install --production
COPY server/unwrapper/unwrapper.js .

ENV PORT=80
ENV BACKEND=ws://127.0.0.1:8443
ENV BACKEND_PATH=/api/v4/sync/data-stream

EXPOSE 80

CMD ["node", "unwrapper.js"]
