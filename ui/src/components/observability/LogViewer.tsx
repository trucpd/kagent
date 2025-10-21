"use client";

import { useEffect, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { AnimatePresence, motion } from "framer-motion";

export function LogViewer() {
  const [logs, setLogs] = useState<string[]>([]);

  useEffect(() => {
    // In a real implementation, we would connect to a log streaming endpoint.
    // For now, we'll just simulate log messages being added.
    const interval = setInterval(() => {
      setLogs((prevLogs) => [...prevLogs.slice(-999), `[${new Date().toISOString()}] Log message`]);
    }, 2000);

    return () => clearInterval(interval);
  }, []);

  return (
    <ScrollArea className="h-64 w-full rounded-md border p-4">
      <AnimatePresence>
        {logs.map((log) => (
          <motion.div
            key={log}
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            className="text-sm"
          >
            {log}
          </motion.div>
        ))}
      </AnimatePresence>
    </ScrollArea>
  );
}
