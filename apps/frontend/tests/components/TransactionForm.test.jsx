import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import TransactionForm from '../../src/features/transactions/TransactionForm';

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

describe('TransactionForm', () => {
  test('renders transaction form with all fields', () => {
    render(<TransactionForm />, { wrapper: createWrapper() });

    expect(screen.getByLabelText(/Transaction Type/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Wallet/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Asset/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Amount/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Date/i)).toBeInTheDocument();
  });

  test('shows validation error when submitting empty form', async () => {
    render(<TransactionForm />, { wrapper: createWrapper() });

    const submitButton = screen.getByRole('button', { name: /Record Transaction/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/select a wallet/i)).toBeInTheDocument();
    });
  });

  test('shows manual price field when checkbox is checked', () => {
    render(<TransactionForm />, { wrapper: createWrapper() });

    const checkbox = screen.getByLabelText(/Manually enter USD price/i);
    fireEvent.click(checkbox);

    expect(screen.getByLabelText(/USD Price per Unit/i)).toBeInTheDocument();
  });

  test('calls onSuccess callback after successful submission', async () => {
    const onSuccess = jest.fn();
    // TODO: Mock API call and test success flow
    // This requires mocking transactionService.create
  });

  test('displays error message on failed submission', async () => {
    // TODO: Mock API call to return error and test error display
  });
});
