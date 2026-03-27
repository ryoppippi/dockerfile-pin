ARG NODE_VERSION=20.11.1
FROM node:${NODE_VERSION}
FROM python:3.12-slim AS builder
FROM --platform=linux/amd64 golang:1.22
FROM scratch
FROM builder AS final
FROM node:20.11.1@sha256:d938c1761e3afbae9242848ffbb95b9cc1cb0a24d889f8bd955204d347a7266e
