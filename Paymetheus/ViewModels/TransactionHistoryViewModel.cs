// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;

namespace Paymetheus.ViewModels
{
    public sealed class TransactionHistoryViewModel : ViewModelBase
    {
        public TransactionHistoryViewModel()
        {
            var synchronizer = ViewModelLocator.SynchronizerViewModel as SynchronizerViewModel;
            if (synchronizer == null)
                return;

            _selectedAccount = synchronizer.Accounts[0];
            PopulateHistoryAsync(_selectedAccount.Account);
        }

        private AccountViewModel _selectedAccount;
        public AccountViewModel SelectedAccount
        {
            get { return _selectedAccount; }
            set
            {
                _selectedAccount = value;

                Transactions.Clear();
                PopulateHistoryAsync(value.Account);

                RaisePropertyChanged();
            }
        }

        private Amount _debitSum;
        public Amount DebitSum
        {
            get { return _debitSum; }
            private set { _debitSum = value; RaisePropertyChanged(); }
        }

        private Amount _creditSum;
        public Amount CreditSum
        {
            get { return _creditSum; }
            private set { _creditSum = value; RaisePropertyChanged(); }
        }

        public sealed class HistoryItem
        {
            public HistoryItem(TransactionViewModel txvm, Amount accountDebit, Amount accountCredit, Amount runningBalance)
            {
                Transaction = txvm;
                AccountDebit = accountDebit;
                AccountCredit = accountCredit;
                RunningBalance = runningBalance;
            }

            public TransactionViewModel Transaction { get; }
            public Amount AccountDebit { get; }
            public Amount AccountCredit { get; }
            public Amount AccountDebitCredit => AccountDebit + AccountCredit;
            public Amount RunningBalance { get; }
        }

        public ObservableCollection<HistoryItem> Transactions { get; } = new ObservableCollection<HistoryItem>();

        // TODO: Figure out what to do with exceptions.  another message box?
        private Task PopulateHistoryAsync(Account account) => Task.Run(() => PopulateHistory(account));

        private void PopulateHistory(Account account)
        {
            var synchronizer = ViewModelLocator.SynchronizerViewModel as SynchronizerViewModel;
            var wallet = synchronizer?.Wallet;
            if (wallet == null)
                return;

            Amount totalDebits = 0;
            Amount totalCredits = 0;
            foreach (var histItem in EnumerateAccountTransactions(wallet, account))
            {
                totalDebits += histItem.AccountDebit;
                totalCredits += histItem.AccountCredit;
                Application.Current.Dispatcher.Invoke(() => Transactions.Add(histItem));
            }

            DebitSum = totalDebits;
            CreditSum = totalCredits;
        }

        private static IEnumerable<HistoryItem> EnumerateAccountTransactions(Wallet wallet, Account account)
        {
            Amount runningBalance = 0;

            // RecentTransactions currently includes every transaction.
            // This will change in a future release, but for now don't bother using RPC to fetch old transactions.
            // Iterate through them, oldest first.
            foreach (var block in wallet.RecentTransactions.MinedTransactions)
            {
                var minedAccountTxs = block.Transactions.
                    Select(tx => AccountTransaction.Create(account, tx)).
                    Where(atx => atx.HasValue).
                    Select(atx => atx.Value);
                foreach (var accountTx in minedAccountTxs)
                {
                    var txvm = new TransactionViewModel(wallet, accountTx.Transaction, block.Identity);
                    runningBalance += accountTx.DebitCredit;
                    yield return new HistoryItem(txvm, accountTx.Debit, accountTx.Credit, runningBalance);
                }
            }

            var unminedAccountTxs = wallet.RecentTransactions.UnminedTransactions.
                Select(tx => AccountTransaction.Create(account, tx.Value)).
                Where(atx => atx.HasValue).
                Select(atx => atx.Value).
                OrderBy(atx => atx.Transaction.SeenTime);
            foreach (var accountTx in unminedAccountTxs)
            {
                var txvm = new TransactionViewModel(wallet, accountTx.Transaction, BlockIdentity.Unmined);
                runningBalance += accountTx.DebitCredit;
                yield return new HistoryItem(txvm, accountTx.Debit, accountTx.Credit, runningBalance);
            }
        }

        public void AppendNewTransactions(Wallet wallet, List<Tuple<WalletTransaction, BlockIdentity>> txs)
        {
            var account = SelectedAccount.Account;
            var totalDebits = DebitSum;
            var totalCredits = CreditSum;
            var runningBalance = totalDebits + totalCredits;
            foreach (var tx in txs)
            {
                var accountTxOption = AccountTransaction.Create(account, tx.Item1);
                if (accountTxOption == null)
                    continue;
                var accountTx = accountTxOption.Value;
                var txvm = new TransactionViewModel(wallet, accountTx.Transaction, tx.Item2);
                totalDebits += accountTx.Debit;
                totalCredits += accountTx.Credit;
                runningBalance += accountTx.DebitCredit;
                var histItem = new HistoryItem(txvm, accountTx.Debit, accountTx.Credit, runningBalance);
                App.Current.Dispatcher.Invoke(() => Transactions.Add(histItem));
            }

            DebitSum = totalDebits;
            CreditSum = totalCredits;
        }
    }
}
