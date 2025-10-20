'use client'

import AgentList from "@/components/AgentList";
import { useSession } from "next-auth/react";
import LoginPage from "./login/page";

export default function AgentListPage() {
  const { data: session } = useSession()

  if (!session) {
    return <LoginPage />
  }

  return <AgentList />;
}
