import api from './api'

interface User {
  id: string
  email: string
  created_at?: string
}

interface AuthResponse {
  token: string
  user: User
}

const authService = {
  async register(email: string, password: string): Promise<AuthResponse> {
    const response = await api.post<AuthResponse>('/auth/register', {
      email,
      password,
    })

    const { token, user } = response.data

    if (token) {
      localStorage.setItem('auth_token', token)
      localStorage.setItem('user', JSON.stringify(user))
    }

    return response.data
  },

  async login(email: string, password: string): Promise<AuthResponse> {
    const response = await api.post<AuthResponse>('/auth/login', {
      email,
      password,
    })

    const { token, user } = response.data

    if (token) {
      localStorage.setItem('auth_token', token)
      localStorage.setItem('user', JSON.stringify(user))
    }

    return response.data
  },

  logout(): void {
    localStorage.removeItem('auth_token')
    localStorage.removeItem('user')
  },

  getCurrentUser(): User | null {
    const userStr = localStorage.getItem('user')
    if (userStr) {
      try {
        return JSON.parse(userStr) as User
      } catch {
        return null
      }
    }
    return null
  },

  getToken(): string | null {
    return localStorage.getItem('auth_token')
  },

  isAuthenticated(): boolean {
    return !!this.getToken()
  },
}

export default authService
