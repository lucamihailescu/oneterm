FROM node:16.20.0-alpine AS builder
WORKDIR /oneterm-ui
COPY . .
# Use npm with the committed package-lock.json for reproducible builds.
# --legacy-peer-deps is required because @wangeditor/editor-for-vue declares
# a peerDep on vue@^2.6.14 while the project is pinned to vue@2.6.11.
RUN npm config set registry https://registry.npmmirror.com/
RUN npm ci --legacy-peer-deps --no-audit --no-fund
RUN npm run build

FROM nginx:alpine
COPY --from=0 /oneterm-ui/dist /etc/nginx/html
RUN mv /etc/nginx/conf.d/default.conf /etc/nginx/conf.d/default.conf.bak 