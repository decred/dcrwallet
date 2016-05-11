// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Wallet;
using Paymetheus.Rpc;
using System;
using System.Collections.ObjectModel;
using System.Linq;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus
{
    sealed class ShellViewModel : ViewModelBase
    {
        public ShellViewModel()
        {
            // Set the window title to the title in the resources assembly.
            // Append network name to window title if not running on mainnet.
            var activeNetwork = App.Current.ActiveNetwork;
            var productTitle = AssemblyResources.Title;
            if (activeNetwork == BlockChainIdentity.MainNet)
            {
                WindowTitle = productTitle;
            }
            else
            {
                WindowTitle = $"{productTitle} [{activeNetwork.Name}]";
            }

            CreateAccountCommand = new DelegateCommand(CreateAccount);
            NavigateBack = new DelegateCommand(ShowRecentActivity);

            _recentActivityViewModel = new RecentActivityViewModel(this);
            VisibleContent = _recentActivityViewModel;

            StartupWizard = new StartupWizard(this);
            StartupWizard.WalletOpened += StartupWizard_WalletOpened;
            StartupWizardVisible = true;
        }

        private Wallet _wallet;
        private readonly RecentActivityViewModel _recentActivityViewModel;
        private AccountViewModel _accountViewModel;

        public string WindowTitle { get; }

        private ViewModelBase _visibleContent;
        public ViewModelBase VisibleContent
        {
            get { return _visibleContent; }
            set { _visibleContent = value; RaisePropertyChanged(); }
        }

        private DialogViewModelBase _visibleDialogContent;
        public DialogViewModelBase VisibleDialogContent
        {
            get { return _visibleDialogContent; }
            set { _visibleDialogContent = value; RaisePropertyChanged(); }
        }

        public DelegateCommand NavigateBack { get; }

        private bool _startupWizardVisible;
        public bool StartupWizardVisible
        {
            get { return _startupWizardVisible; }
            set
            {
                if (_startupWizardVisible != value)
                {
                    _startupWizardVisible = value;
                    RaisePropertyChanged();
                }
            }
        }

        public StartupWizard StartupWizard { get; }

        private async void StartupWizard_WalletOpened(object sender, EventArgs args)
        {
            try
            {
                var syncingWallet = await App.Current.WalletRpcClient.Synchronize(_wallet_ChangesProcessed);
                _wallet = syncingWallet.Item1;
                OnSyncedWallet();

                var syncTask = syncingWallet.Item2;
                await syncTask;
            }
            catch (ConnectTimeoutException)
            {
                MessageBox.Show("Unable to connect to wallet");
            }
            catch (Exception ex)
            {
                var ae = ex as AggregateException;
                if (ae != null)
                {
                    Exception inner;
                    if (ae.TryUnwrap(out inner))
                        ex = inner;
                }

                HandleSyncFault(ex);
            }
            finally
            {
                if (_wallet != null)
                    _wallet.ChangesProcessed -= _wallet_ChangesProcessed;
                StartupWizardVisible = true;
            }
        }

        private void OnSyncedWallet()
        {
            var txSet = _wallet.RecentTransactions;
            var recentTx = txSet.UnminedTransactions
                .Select(x => new TransactionViewModel(_wallet, x.Value, BlockIdentity.Unmined))
                .Concat(txSet.MinedTransactions.ReverseList().SelectMany(b => b.Transactions.Select(tx => new TransactionViewModel(_wallet, tx, b.Identity))))
                .Take(10);
            var recentAccounts = _wallet.EnumrateAccounts()
                .Select(a => new RecentAccountViewModel(this, a.Key, a.Value));
            Application.Current.Dispatcher.Invoke(() =>
            {
                foreach (var tx in recentTx)
                    _recentActivityViewModel.RecentTransactions.Add(tx);
                foreach (var account in recentAccounts)
                    RecentAccounts.Add(account);
            });
            SyncedBlockHeight = _wallet.ChainTip.Height;
            NotifyRecalculatedBalances();
            StartupWizardVisible = false;
            ShowRecentActivity();
        }

        private void NotifyRecalculatedBalances()
        {
            foreach (var recentAccount in RecentAccounts)
            {
                var currentState = _wallet.LookupAccountProperties(recentAccount.Account);
                recentAccount.Balance = currentState.ZeroConfSpendableBalance;
            }

            RaisePropertyChanged(nameof(TotalBalance));

            // If account visible, calculate spendable balance
            if (_visibleContent is AccountViewModel)
            {
                var accountViewModel = (AccountViewModel)_visibleContent;
                var accountProperties = _wallet.LookupAccountProperties(accountViewModel.Account);
                accountViewModel.UpdateAccountProperties(1, accountProperties); // TODO: don't hardcode confs
                Application.Current.Dispatcher.Invoke(() =>
                {
                    accountViewModel.PopulateTransactionHistory();
                });
            }
        }

        private void _wallet_ChangesProcessed(object sender, Wallet.ChangesProcessedEventArgs e)
        {
            var currentHeight = e.NewChainTip?.Height ?? SyncedBlockHeight;

            var movedTxViewModels = _recentActivityViewModel.RecentTransactions
                .Where(txvm => e.MovedTransactions.ContainsKey(txvm.TxHash))
                .Select(txvm => Tuple.Create(txvm, e.MovedTransactions[txvm.TxHash]));

            var newTxViewModels = e.AddedTransactions.Select(tx => new TransactionViewModel(_wallet, tx.Item1, tx.Item2)).ToList();

            foreach (var kvp in e.ModifiedAccountProperties)
            {
                var account = kvp.Key;
                var state = kvp.Value;
                var recentAccountVM = RecentAccounts.FirstOrDefault(vm => vm.Account == account);
                if (recentAccountVM != null)
                {
                    recentAccountVM.AccountName = state.AccountName;
                    recentAccountVM.Balance = state.TotalBalance;
                }
                else
                {
                    Application.Current.Dispatcher.Invoke(() =>
                    {
                        RecentAccounts.Add(new RecentAccountViewModel(this, account, state));
                    });
                }
            }

            RaisePropertyChanged(nameof(TotalBalance));

            if (VisibleContent is AccountViewModel)
            {
                var accountViewModel = (AccountViewModel)VisibleContent;
                AccountProperties accountProperties;
                if (e.ModifiedAccountProperties.TryGetValue(accountViewModel.Account, out accountProperties))
                {
                    accountViewModel.UpdateAccountProperties(1, accountProperties);
                }
            }

            foreach (var movedTx in movedTxViewModels)
            {
                var txvm = movedTx.Item1;
                var location = movedTx.Item2;

                txvm.Location = location;
                txvm.Confirmations = BlockChain.Confirmations(currentHeight, location);
            }

            Application.Current.Dispatcher.Invoke(() =>
            {
                foreach (var txvm in newTxViewModels)
                {
                    _recentActivityViewModel.RecentTransactions.Insert(0, txvm);
                }
            });

            if (e.NewChainTip != null)
            {
                SyncedBlockHeight = ((BlockIdentity)(e.NewChainTip)).Height;
            }
        }

        private static void HandleSyncFault(Exception ex)
        {
            string message;
            var shutdown = false;

            // Sync task ended.  Decide whether to restart the task and sync a
            // fresh wallet, or error out with an explanation.
            if (ErrorHandling.IsTransient(ex))
            {
                // This includes network issues reaching the wallet, like disconnects.
                message = $"A temporary error occurred, but reconnecting is not implemented.\n\n{ex}";
                shutdown = true; // TODO: implement reconnect logic.
            }
            else if (ErrorHandling.IsServerError(ex))
            {
                message = $"The wallet failed to service a request.\n\n{ex}";
            }
            else if (ErrorHandling.IsClientError(ex))
            {
                message = $"A client request could not be serviced.\n\n{ex}";
            }
            else
            {
                message = $"An unexpected error occurred:\n\n{ex}";
                shutdown = true;
            }

            Application.Current.Dispatcher.Invoke(() =>
            {
                MessageBox.Show(message, "Error");
                if (shutdown)
                    Application.Current.Shutdown();
            });
        }

        private int _syncedBlockHeight;
        public int SyncedBlockHeight
        {
            get { return _syncedBlockHeight; }
            set { _syncedBlockHeight = value; RaisePropertyChanged(); }
        }

        public ObservableCollection<RecentAccountViewModel> RecentAccounts { get; } =
            new ObservableCollection<RecentAccountViewModel>();

        public Amount TotalBalance => _wallet?.TotalBalance ?? 0;

        public ICommand CreateAccountCommand { get; }

        private void CreateAccount()
        {
            VisibleDialogContent = new CreateAccountDialogViewModel(this);
        }

        protected override bool OnRoutedMessage(ViewModelBase sender, ViewModelMessageBase message)
        {
            var showTx = message as ShowTransactionMessage;
            if (showTx != null)
            {
                ShowTransaction(showTx.Transaction);
                return true;
            }

            var showAccount = message as ShowAccountMessage;
            if (showAccount != null)
            {
                ShowAccount(showAccount.Account);
                return true;
            }

            var openDialog = message as OpenDialogMessage;
            if (openDialog != null && sender == VisibleContent)
            {
                VisibleDialogContent = openDialog.Dialog;
                return true;
            }

            var hideDialog = message as HideDialogMessage;
            if (hideDialog != null && sender == VisibleDialogContent)
            {
                VisibleDialogContent = null;
                return true;
            }

            return false;
        }

        private void ShowRecentActivity()
        {
            VisibleContent = _recentActivityViewModel;
        }

        private void ShowTransaction(TransactionViewModel tx)
        {
            VisibleContent = tx;
        }

        private void ShowAccount(Account account)
        {
            // TODO: disconnect signals for previous account VM if exists.
            var accountName = _wallet.AccountName(account);
            _accountViewModel = new AccountViewModel(this, _wallet, account);

            VisibleContent = _accountViewModel;
        }
    }
}
