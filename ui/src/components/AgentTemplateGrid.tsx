"use client";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { AgentTemplateResponse } from "@/types";
import { useRouter } from "next/navigation";

interface AgentTemplateGridProps {
  agentTemplateResponse: AgentTemplateResponse[];
}

export function AgentTemplateGrid({ agentTemplateResponse }: AgentTemplateGridProps) {
  const router = useRouter();

  const handleCardClick = (template: AgentTemplateResponse) => {
    const encodedTemplate = encodeURIComponent(JSON.stringify(template.agentTemplate));
    router.push(`/agents/new?template=${encodedTemplate}`);
  };

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      {agentTemplateResponse.map((template) => (
        <Card
          key={template.agentTemplate.metadata.name}
          className="hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer"
          onClick={() => handleCardClick(template)}
        >
          <CardHeader>
            <CardTitle>{template.agentTemplate.metadata.name}</CardTitle>
            <CardDescription>{template.agentTemplate.spec.description}</CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-gray-500">Author: {template.agentTemplate.spec.author}</p>
            <div className="flex flex-wrap gap-2 mt-2">
              {template.agentTemplate.spec.tags?.map((tag) => (
                <span key={tag} className="bg-gray-200 text-gray-700 px-2 py-1 rounded-full text-xs">
                  {tag}
                </span>
              ))}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
