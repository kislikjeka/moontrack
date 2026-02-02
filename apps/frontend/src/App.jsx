import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider } from './features/auth/AuthContext'
import ProtectedRoute from './features/auth/ProtectedRoute'
import ErrorBoundary from './components/ErrorBoundary'
import LoginForm from './features/auth/LoginForm'
import RegistrationForm from './features/auth/RegistrationForm'
import Layout from './components/layout/Layout'
import Dashboard from './features/dashboard/Dashboard'
import WalletList from './features/wallets/WalletList'
import WalletDetail from './features/wallets/WalletDetail'
import TransactionForm from './features/transactions/TransactionForm'
import TransactionList from './features/transactions/TransactionList'
import './App.css'

const queryClient = new QueryClient()

function App() {
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <Router>
          <AuthProvider>
          <Routes>
            {/* Public routes */}
            <Route path="/login" element={<LoginForm />} />
            <Route path="/register" element={<RegistrationForm />} />

            {/* Protected routes with Layout */}
            <Route
              path="/dashboard"
              element={
                <ProtectedRoute>
                  <Layout>
                    <Dashboard />
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
                    <WalletList />
                  </Layout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/wallets/:id"
              element={
                <ProtectedRoute>
                  <Layout>
                    <WalletDetail />
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
                    <TransactionList />
                  </Layout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/transactions/new"
              element={
                <ProtectedRoute>
                  <Layout>
                    <TransactionForm />
                  </Layout>
                </ProtectedRoute>
              }
            />

            {/* Default redirect */}
            <Route path="/" element={<Navigate to="/dashboard" replace />} />

            {/* 404 catch-all */}
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Routes>
          </AuthProvider>
        </Router>
      </QueryClientProvider>
    </ErrorBoundary>
  )
}

export default App
