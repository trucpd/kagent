"use client";

import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface Metric {
  name: string;
  value: string;
}

export function MetricsDashboard() {
  const [metrics, setMetrics] = useState<Metric[]>([]);

  useEffect(() => {
    // In a real implementation, we would fetch metrics from a backend endpoint.
    // For now, we'll just use dummy data.
    setMetrics([
      { name: "CPU Usage", value: "25%" },
      { name: "Memory Usage", value: "512MB" },
      { name: "Requests per Second", value: "120" },
    ]);
  }, []);

  return (
    <div className="grid gap-4 md:grid-cols-3">
      {metrics.map((metric) => (
        <Card key={metric.name}>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">{metric.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{metric.value}</div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
