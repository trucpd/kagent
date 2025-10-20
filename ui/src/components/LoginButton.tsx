'use client'

import { useSession, signIn, signOut } from 'next-auth/react'
import { Button } from '@/components/ui/button'

export default function LoginButton() {
  const { data: session } = useSession()

  if (session) {
    return (
      <div className="flex items-center gap-4">
        <p>{session.user?.name}</p>
        <Button onClick={() => signOut()}>Logout</Button>
      </div>
    )
  }

  return <Button onClick={() => signIn()}>Login</Button>
}
