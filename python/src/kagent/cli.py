import logging
import os
from typing import Annotated

import typer
import uvicorn
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from kagent.models import build_app

app = typer.Typer()


@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    filepath: str = "/config/config.json",
    reload: Annotated[bool, typer.Option("--reload")] = False,
):
    tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
    if tracing_enabled:
        logging.info("Enabling tracing")
        tracer_provider = TracerProvider(resource=Resource({"service.name": "kagent"}))
        processor = BatchSpanProcessor(OTLPSpanExporter())
        tracer_provider.add_span_processor(processor)
        trace.set_tracer_provider(tracer_provider)
        HTTPXClientInstrumentor().instrument()
        OpenAIInstrumentor().instrument()

    uvicorn.run(
        build_app(filepath),
        host=host,
        port=port,
        workers=workers,
        reload=reload,
    )


def run():
    logging.basicConfig(level=logging.INFO)
    logging.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run()
