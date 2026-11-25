/// <reference types="vite/client" />
interface ImportMetaEnv {
  readonly VITE_REPO_URL: string
  readonly VITE_GIT_SHA: string
}
declare module '*.md?raw' {
  const text: string
  export default text
}
