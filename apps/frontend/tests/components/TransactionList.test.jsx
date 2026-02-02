import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import TransactionList from '../../src/features/transactions/TransactionList';

const createWrapper = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return ({ children }) => (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  );
};

describe('TransactionList', () => {
  test('renders loading state initially', () => {
    render(<TransactionList />, { wrapper: createWrapper() });
    expect(screen.getByText(/Loading transactions/i)).toBeInTheDocument();
  });

  test('renders empty state when no transactions', async () => {
    // TODO: Mock API to return empty list
    // expect(screen.getByText(/No transactions found/i)).toBeInTheDocument();
  });

  test('renders list of transactions', async () => {
    // TODO: Mock API to return transaction list
    // await waitFor(() => {
    //   expect(screen.getByText(/Transaction History/i)).toBeInTheDocument();
    // });
  });

  test('filters transactions by asset', async () => {
    // TODO: Test filter functionality
  });

  test('handles pagination correctly', async () => {
    // TODO: Test pagination buttons
  });

  test('displays error message on failed load', async () => {
    // TODO: Mock API error and test error display
  });
});
