import {
  createContext,
  useState,
  useContext,
  useEffect,
  type ReactNode,
} from 'react'
import authService from '@/services/auth'

interface User {
  id: string
  email: string
  created_at?: string
}

interface AuthResult {
  success: boolean
  data?: { token: string; user: User }
  error?: string
}

interface AuthContextValue {
  user: User | null
  loading: boolean
  register: (email: string, password: string) => Promise<AuthResult>
  login: (email: string, password: string) => Promise<AuthResult>
  logout: () => void
  isAuthenticated: () => boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

interface AuthProviderProps {
  children: ReactNode
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const currentUser = authService.getCurrentUser()
    if (currentUser) {
      setUser(currentUser)
    }
    setLoading(false)
  }, [])

  const register = async (email: string, password: string): Promise<AuthResult> => {
    try {
      const data = await authService.register(email, password)
      setUser(data.user)
      return { success: true, data }
    } catch (error: unknown) {
      const axiosError = error as { response?: { data?: { error?: string } } }
      const message = axiosError.response?.data?.error || 'Registration failed'
      return { success: false, error: message }
    }
  }

  const login = async (email: string, password: string): Promise<AuthResult> => {
    try {
      const data = await authService.login(email, password)
      setUser(data.user)
      return { success: true, data }
    } catch (error: unknown) {
      const axiosError = error as { response?: { data?: { error?: string } } }
      const message = axiosError.response?.data?.error || 'Login failed'
      return { success: false, error: message }
    }
  }

  const logout = () => {
    authService.logout()
    setUser(null)
  }

  const isAuthenticated = () => {
    return !!user && authService.isAuthenticated()
  }

  const value: AuthContextValue = {
    user,
    loading,
    register,
    login,
    logout,
    isAuthenticated,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

export default AuthContext
