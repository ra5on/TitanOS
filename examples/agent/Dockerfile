# TitanOS agent: Python standard library only, no packages to install.
FROM python:3.13-slim

LABEL org.opencontainers.image.title="TitanOS agent" \
      org.opencontainers.image.description="Chat with a local Ollama model about the state of a TitanOS NAS, over its read-only MCP tools" \
      org.opencontainers.image.source="https://github.com/ra5on/TitanOS" \
      org.opencontainers.image.licenses="FSL-1.1-ALv2"

RUN useradd --system --uid 10001 --no-create-home agent
WORKDIR /app
COPY agent.py ./
COPY static ./static

USER agent
ENV PYTHONUNBUFFERED=1 AGENT_PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD ["python3", "-c", "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8080/healthz').status == 200 else 1)"]
CMD ["python3", "agent.py"]
