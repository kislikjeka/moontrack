import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider } from '@/features/auth/AuthContext'
import ProtectedRoute from '@/features/auth/ProtectedRoute'
import { Toaster } from '@/components/ui/sonner'

// Pages - will be implemented in later phases
import LoginPage from '@/features/auth/LoginPage'
import RegisterPage from '@/features/auth/RegisterPage'
import DashboardPage from '@/features/dashboard/DashboardPage'
import WalletsPage from '@/features/wallets/WalletsPage'
import WalletDetailPage from '@/features/wallets/WalletDetailPage'
import TransactionsPage from '@/features/transactions/TransactionsPage'
import TransactionFormPage from '@/features/transactions/TransactionFormPage'
import TransactionDetailPage from '@/features/transactions/TransactionDetailPage'
import SettingsPage from '@/features/settings/SettingsPage'
import { Layout } from '@/components/layout/Layout'

function App() {
  return (
    <Router>
      <AuthProvider>
        <Routes>
          {/* Public routes */}
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />

          {/* Protected routes with Layout */}
          <Route
            path="/dashboard"
            element={
              <ProtectedRoute>
                <Layout>
                  <DashboardPage />
                </Layout>
              </ProtectedRoute>
            }
          />

          {/* Wallet routes */}
          <Route
            path="/wallets"
            element={
              <ProtectedRoute>
                <Layout>
                  <WalletsPage />
                </Layout>
              </ProtectedRoute>
            }
          />
          <Route
            path="/wallets/:id"
            element={
              <ProtectedRoute>
                <Layout>
                  <WalletDetailPage />
                </Layout>
              </ProtectedRoute>
            }
          />

          {/* Transaction routes */}
          <Route
            path="/transactions"
            element={
              <ProtectedRoute>
                <Layout>
                  <TransactionsPage />
                </Layout>
              </ProtectedRoute>
            }
          />
          <Route
            path="/transactions/new"
            element={
              <ProtectedRoute>
                <Layout>
                  <TransactionFormPage />
                </Layout>
              </ProtectedRoute>
            }
          />
          <Route
            path="/transactions/:id"
            element={
              <ProtectedRoute>
                <Layout>
                  <TransactionDetailPage />
                </Layout>
              </ProtectedRoute>
            }
          />

          {/* Settings */}
          <Route
            path="/settings"
            element={
              <ProtectedRoute>
                <Layout>
                  <SettingsPage />
                </Layout>
              </ProtectedRoute>
            }
          />

          {/* Default redirects */}
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
        <Toaster />
      </AuthProvider>
    </Router>
  )
}

export default App
