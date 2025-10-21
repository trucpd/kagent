"use client";

import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

interface Span {
  spanId: string;
  operationName: string;
  startTime: string;
  duration: string;
}

interface Trace {
  traceId: string;
  spans: Span[];
}

export function TraceVisualization() {
  const [traces, setTraces] = useState<Trace[]>([]);
  const [expandedTraces, setExpandedTraces] = useState<Record<string, boolean>>({});

  useEffect(() => {
    async function fetchTraces() {
      try {
        const response = await fetch("/api/observability/traces");
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();
        setTraces(data);
      } catch (error) {
        console.error("Failed to fetch traces:", error);
      }
    }

    fetchTraces();
  }, []);

  const toggleTraceExpansion = (traceId: string) => {
    setExpandedTraces((prev) => ({ ...prev, [traceId]: !prev[traceId] }));
  };

  return (
    <div className="w-full">
      <AnimatePresence>
        {traces.map((trace) => (
          <motion.div
            key={trace.traceId}
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            className="mb-2"
          >
            <div
              className="flex items-center justify-between p-2 rounded-md bg-muted/50 cursor-pointer"
              onClick={() => toggleTraceExpansion(trace.traceId)}
            >
              <span className="text-sm font-medium">{trace.traceId}</span>
              <ChevronRight
                className={cn(
                  "h-4 w-4 transition-transform duration-200",
                  expandedTraces[trace.traceId] && "rotate-90"
                )}
              />
            </div>
            {expandedTraces[trace.traceId] && (
              <div className="p-2 border-l-2 border-primary/50 ml-2">
                {trace.spans.map((span) => (
                  <div key={span.spanId} className="text-sm text-muted-foreground mb-1">
                    <strong>{span.operationName}</strong> ({span.duration})
                  </div>
                ))}
              </div>
            )}
          </motion.div>
        ))}
      </AnimatePresence>
    </div>
  );
}
