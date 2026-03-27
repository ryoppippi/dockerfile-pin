# Go multi-stage (builder -> distroless)
FROM golang:1.26.1-trixie AS builder
RUN go build -o /app
FROM gcr.io/distroless/static:01b9ed74ee38468719506f73b50d7bd8e596c37b
COPY --from=builder /app /app

# Node.js with Debian variant
FROM node:24.14.0-bookworm-slim AS build
RUN npm ci
FROM node:24.14.0-bookworm-slim AS runner

# Python uv multi-stage (GHCR + complex tag)
FROM ghcr.io/astral-sh/uv:0.10.9-python3.13-trixie AS uv-builder
RUN uv sync
FROM python:3.13-slim-trixie AS runtime

# tag-less (implicit :latest)
FROM ubuntu

# latest tag explicitly
FROM headscale/headscale:latest

# specific registry with port
FROM registry.example.com:5000/myapp:1.0

# ECR image
FROM 123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp:latest

# ARG with default for tag
ARG PYTHON_TAG=3.12-slim
FROM python:${PYTHON_TAG} AS deps

# ARG for registry
ARG REGISTRY=docker.io
FROM ${REGISTRY}/nginx:1.25

# ARG without default (skip)
ARG CUSTOM_BASE
FROM ${CUSTOM_BASE}

# digest-only (already pinned)
FROM alpine@sha256:abcdef1234567890

# PostgreSQL with variant
FROM postgres:16.6-bookworm

# Debian slim
FROM debian:bookworm-20250407-slim

# distroless nonroot
FROM gcr.io/distroless/static-debian12:nonroot
