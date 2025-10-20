'use client'

import { useSession, signIn } from 'next-auth/react'
import { useEffect } from 'react'

export default function LoginPage() {
  const { data: session, status } = useSession()

  useEffect(() => {
    if (status === 'loading') return
    if (status === 'unauthenticated') {
      signIn()
    }
    if (status === 'authenticated') {
      window.location.href = '/'
    }
  }, [status])

  return <div>Loading...</div>
}
