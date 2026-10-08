import { useEffect, useState } from 'react'
import { System } from './mygo'

export default function App() {
  const [info, setInfo] = useState<Record<string, string>>({})
  useEffect(() => { void System.info().then(setInfo) }, [])
  return <div className="p-6 text-sm">Orole OS · {info.os}/{info.arch}</div>
}
