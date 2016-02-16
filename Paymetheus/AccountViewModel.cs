// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Script;
using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using System;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus
{
    sealed class AccountViewModel : ViewModelBase
    {
        public AccountViewModel(ShellViewModel shell, Wallet wallet, Account account)
            : base(shell)
        {
            _wallet = wallet;
            _account = account;
            _accountProperties = _wallet.LookupAccountProperties(_account);
            UpdateAccountProperties(1, _accountProperties); // TODO: Don't hardcode confs

            RenameAccount = new DelegateCommand(RenameAcountAction);
            ImportKey = new DelegateCommand(ImportKeyAction);

            PopulateTransactionHistory();

            FetchUnspentOutputs = new DelegateCommand(FetchUnspentOutputsAction);

            AddPendingOutput = new ButtonCommand("Add output", AddPendingOutputAction);
            RemovePendingOutput = new DelegateCommand<PendingOutput>(RemovePendingOutputAction);
            FinishCreateTransaction = new ButtonCommand("Send transaction", FinishCreateTransactionAction);
            AddPendingOutputAction();
            RecalculateCreateTransaction();

            RequestAddress = new ButtonCommand("Create single-use address", RequestAddressAction);
        }

        private readonly Wallet _wallet;
        private readonly Account _account;
        private AccountProperties _accountProperties;

        public Account Account => _account;
        public string CurrencyTicker => Denomination.Decred.Ticker; // TODO: Denomination should be modifiable by user

        public void UpdateAccountProperties(int requiredConfirmations, AccountProperties props)
        {
            RaisePropertyChanged(nameof(TotalBalance));
            SpendableBalance = _wallet.CalculateSpendableBalance(_account, requiredConfirmations);

            ExternalAddressCount = props.ExternalKeyCount;
            InternalAddressCount = props.InternalKeyCount;
            ImportedAddressCount = props.ImportedKeyCount;
        }

        #region Summary
        public uint CoinType => _wallet.ActiveChain.Bip44CoinType;
        public uint AccountNumber => _account.AccountNumber;

        public Amount TotalBalance => _accountProperties.TotalBalance;
        private Amount _spendableBalance;
        public Amount SpendableBalance
        {
            get { return _spendableBalance; }
            set { _spendableBalance = value; RaisePropertyChanged(); }
        }

        private uint _externalAddressCount;
        private uint _internalAddressCount;
        private uint _importedAddressCount;
        public uint ExternalAddressCount
        {
            get { return _externalAddressCount; }
            set { if (_externalAddressCount != value) { _externalAddressCount = value; RaisePropertyChanged(); } }
        }
        public uint InternalAddressCount
        {
            get { return _internalAddressCount; }
            set { if (_internalAddressCount != value) { _internalAddressCount = value; RaisePropertyChanged(); } }
        }
        public uint ImportedAddressCount
        {
            get { return _importedAddressCount; }
            set { if (_importedAddressCount != value) { _importedAddressCount = value; RaisePropertyChanged(); } }
        }

        public ICommand RenameAccount { get; }
        public ICommand ImportKey { get; }

        private void RenameAcountAction()
        {
            var dialog = new RenameAccountDialogViewModel((ShellViewModel)_parentViewModel, _account, _wallet.AccountName(_account));
            var message = new OpenDialogMessage(dialog);
            PostMessage(message);
        }

        private void ImportKeyAction()
        {
            var dialog = new ImportDialogViewModel((ShellViewModel)_parentViewModel, _account);
            var message = new OpenDialogMessage(dialog);
            PostMessage(message);
        }
        #endregion

        #region TransactionHistory
        public class AccountTransactionViewModel : ViewModelBase
        {
            public AccountTransactionViewModel(WalletTransaction transaction, Amount runningBalanceDelta, Amount runningBalance)
            {
                _transaction = transaction;
                BalanceDelta = runningBalanceDelta;
                _balance = runningBalance;
            }

            private WalletTransaction _transaction;
            public Blake256Hash TxHash => _transaction.Hash;
            public DateTimeOffset LocalSeenTime => _transaction.SeenTime.LocalDateTime;
            public Amount BalanceDelta { get; }
            public Amount? AccountDebit => BalanceDelta < 0 ? BalanceDelta : (Amount?)null;
            public Amount? AccountCredit => BalanceDelta < 0 ? (Amount?)null : BalanceDelta;
            private Amount _balance;
            public Amount Balance
            {
                get { return _balance; }
                set { _balance = value; RaisePropertyChanged(); }
            }
        }


        public ObservableCollection<AccountTransactionViewModel> TransactionHistory { get; } =
            new ObservableCollection<AccountTransactionViewModel>();

        public void PopulateTransactionHistory()
        {
            TransactionHistory.Clear();

            // TODO: don't require every wallet transaction.  Consider alternate source for these
            // transactions.
            var walletTxs = _wallet.RecentTransactions.MinedTransactions
                .SelectMany(b => b.Transactions)
                .Concat(_wallet.RecentTransactions.UnminedTransactions.Select(kvp => kvp.Value));
            Amount runningBalance = 0;
            foreach (var tx in walletTxs)
            {
                Amount debit, credit;
                if (Accounting.RelevantTransaction(tx, _account, out debit, out credit))
                {
                    Amount delta = debit + credit;
                    runningBalance += delta;

                    var accountTransaction = new AccountTransactionViewModel(tx, delta, runningBalance);
                    TransactionHistory.Add(accountTransaction);
                }
            }
        }
        #endregion

        #region UnspentOutputs
        public ICommand FetchUnspentOutputs { get; }

        private async void FetchUnspentOutputsAction()
        {
            var unspentOutputs = await App.Current.WalletRpcClient.SelectUnspentOutputs(_account, 0, 0);
            var allUnspentOutputs = unspentOutputs.Item1;

            Application.Current.Dispatcher.Invoke(() =>
            {
                UnspentOutputs.Clear();
                foreach (var unspentOutputVM in allUnspentOutputs)
                {
                    UnspentOutputs.Add(unspentOutputVM);
                }
            });
        }

        public ObservableCollection<UnspentOutput> UnspentOutputs { get; } =
            new ObservableCollection<UnspentOutput>();
        #endregion

        #region CreateTransaction
        private bool _manualInputSelection;
        public bool ManualInputSelection
        {
            get { return _manualInputSelection; }
            set { _manualInputSelection = value; RaisePropertyChanged(); }
        }

        // TODO: input types for manual input selection.

        public class PendingOutput
        {
            public enum Kind
            {
                Address,
                Script,
            }

            private Kind _outputType;
            public Kind OutputType
            {
                get { return _outputType; }
                set { _outputType = value; RaiseChanged(); }
            }

            private string _destination;
            public string Destination
            {
                get { return _destination; }
                set { _destination = value; RaiseChanged(); }
            }

            public Amount _outputAmount;
            public Amount OutputAmount
            {
                get { return _outputAmount; }
                set
                {
                    if (value != _outputAmount && TransactionRules.IsSaneOutputValue(value))
                    {
                        _outputAmount = value;
                        RaiseChanged();
                    }
                }
            }

            /// <summary>
            /// Checks whether all user-set fields of the pending output are ready to be used
            /// to create a transaction output.
            /// </summary>
            /// <returns>Validity of the pending output</returns>
            public bool IsValid()
            {
                return !string.IsNullOrWhiteSpace(Destination);
            }

            public event EventHandler Changed;

            private void RaiseChanged()
            {
                Changed?.Invoke(this, EventArgs.Empty);
            }

            public OutputScript BuildOutputScript()
            {
                switch (OutputType)
                {
                    case Kind.Address:
                        var address = Address.Decode(Destination);
                        return address.BuildScript();

                    case Kind.Script:
                        return new OutputScript.Unrecognized(Hexadecimal.Decode(Destination));

                    default:
                        throw new Exception($"Unknown pending output kind {OutputType}");
                }
            }
        }

        public ObservableCollection<PendingOutput> PendingOutputs { get; } = new ObservableCollection<PendingOutput>();

        public ICommand RemovePendingOutput { get; }

        private void RemovePendingOutputAction(PendingOutput item)
        {
            if (PendingOutputs.Remove(item))
            {
                item.Changed -= PendingOutput_Changed;
                RecalculateCreateTransaction();
            }
        }

        public ButtonCommand AddPendingOutput { get; }

        private void AddPendingOutputAction()
        {
            var pendingOutput = new PendingOutput();
            pendingOutput.Changed += PendingOutput_Changed;
            PendingOutputs.Add(pendingOutput);

            RecalculateCreateTransaction();
        }

        private void PendingOutput_Changed(object sender, EventArgs e)
        {
            RecalculateCreateTransaction();
        }

        private void RecalculateCreateTransaction()
        {
            if (PendingOutputs.Count > 0 && PendingOutputs.All(x => x.IsValid()))
            {
                EstimatedFee = 0;
                EstimatedRemainingBalance = 0;
                FinishCreateTransaction.Executable = true;
            }
            else
            {
                FinishCreateTransaction.Executable = false;
            }
        }

        private Amount _estimatedRemainingBalance;
        public Amount EstimatedRemainingBalance
        {
            get { return _estimatedRemainingBalance; }
            set { _estimatedRemainingBalance = value; RaisePropertyChanged(); }
        }

        private Amount _estimatedFee;
        public Amount EstimatedFee
        {
            get { return _estimatedFee; }
            set { _estimatedFee = value; RaisePropertyChanged(); }
        }

        private bool _signTransaction = true;
        public bool SignTransaction
        {
            get { return _signTransaction; }
            set
            {
                if (_signTransaction != value)
                {
                    _signTransaction = value;
                    RaisePropertyChanged();
                    PublishActive = value;
                    FinishCreateTransaction.ButtonLabel = FinishCreateTransactionText();
                }
            }
        }

        public bool _publishTransaction = true;
        public bool PublishTransaction
        {
            get { return _publishActive && _publishTransaction; }
            set
            {
                if (_publishTransaction != value)
                {
                    _publishTransaction = value;
                    RaisePropertyChanged();
                    FinishCreateTransaction.ButtonLabel = FinishCreateTransactionText();
                }
            }
        }

        private bool _publishActive = true;
        public bool PublishActive
        {
            get { return _publishActive; }
            set
            {
                _publishActive = value;
                RaisePropertyChanged();
                RaisePropertyChanged(nameof(PublishTransaction));
            }
        }

        public ButtonCommand FinishCreateTransaction { get; }

        private void FinishCreateTransactionAction()
        {
            try
            {
                if (SignTransaction)
                {
                    var outputs = PendingOutputs.Select(po =>
                    {
                        var script = po.BuildOutputScript().Script;
                        return new Transaction.Output(po.OutputAmount, Transaction.Output.LatestPkScriptVersion, script);
                    }).ToArray();
                    var shell = (ShellViewModel)_parentViewModel;
                    Func<string, Task> action = (passphrase) => SignTransactionWithPassphrase(passphrase, outputs, PublishTransaction);
                    var dialog = new PassphraseDialogViewModel(shell, "Enter passphrase to sign transaction", "Sign", action);
                    PostMessage(new OpenDialogMessage(dialog));
                }
                else
                {
                    ShowUnsignedTransaction();
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message);
            }
        }

        private void ShowUnsignedTransaction()
        {
            MessageBox.Show("unimplemented :(");
        }

        private async Task SignTransactionWithPassphrase(string passphrase, Transaction.Output[] outputs, bool publishImmediately)
        {
            var walletClient = App.Current.WalletRpcClient;
            var requiredConfirmations = 1; // TODO: Don't hardcode confs.
            var targetAmount = outputs.Sum(o => o.Amount);
            var targetFee = (Amount)1e4; // TODO: Don't hardcode fee/kB.
            var funding = await walletClient.FundTransactionAsync(_account, targetAmount + targetFee, requiredConfirmations);
            var fundingAmount = funding.Item2;
            if (fundingAmount < targetAmount + targetFee)
            {
                MessageBox.Show($"Transaction requires {targetAmount + targetFee} but only {fundingAmount} is spendable.",
                    "Insufficient funds to create transaction.");
                return;
            }

            var selectedOutputs = funding.Item1;
            var inputs = selectedOutputs
                .Select(o =>
                {
                    var prevOutPoint = new Transaction.OutPoint(o.TransactionHash, o.OutputIndex, 0);
                    return Transaction.Input.CreateFromPrefix(prevOutPoint, TransactionRules.MaxInputSequence);
                })
                .ToArray();

            // TODO: Port the fee estimation logic from btcwallet.  Using a hardcoded fee is unacceptable.
            var estimatedFee = targetFee;

            var changePkScript = funding.Item3;
            if (changePkScript != null)
            {
                // Change output amount is calculated by solving for changeAmount with the equation:
                //   estimatedFee = fundingAmount - (targetAmount + changeAmount)
                var changeOutput = new Transaction.Output(fundingAmount - targetAmount - estimatedFee,
                    Transaction.Output.LatestPkScriptVersion, changePkScript.Script);
                var outputsList = outputs.ToList();
                // TODO: Randomize change output position.
                outputsList.Add(changeOutput);
                outputs = outputsList.ToArray();
            }

            // TODO: User may want to set the locktime.
            var unsignedTransaction = new Transaction(Transaction.SupportedVersion, inputs, outputs, 0, 0);

            var signingResponse = await walletClient.SignTransactionAsync(passphrase, unsignedTransaction);
            var complete = signingResponse.Item2;
            if (!complete)
            {
                MessageBox.Show("Failed to create transaction input signatures.");
                return;
            }
            var signedTransaction = signingResponse.Item1;

            MessageBox.Show($"Created tx with {estimatedFee} fee.");

            if (!publishImmediately)
            {
                MessageBox.Show("Reviewing signed transaction before publishing is not implemented yet.");
                return;
            }

            // TODO: The client just deserialized the transaction, so serializing it is a
            // little silly.  This could be optimized.
            await walletClient.PublishTransactionAsync(signedTransaction.Serialize());
            MessageBox.Show("Published transaction.");
        }

        private string FinishCreateTransactionText()
        {
            if (SignTransaction && PublishTransaction)
                return "Send transaction";
            else if (SignTransaction)
                return "Show signed transaction";
            else
                return "Show unsigned transaction";
        }
        #endregion

        #region ReceivePayment
        public ButtonCommand RequestAddress { get; }
        private string _createdAddress = "";
        public string CreatedAddress
        {
            get { return _createdAddress; }
            set { _createdAddress = value; RaisePropertyChanged(); }
        }

        private async void RequestAddressAction()
        {
            try
            {
                RequestAddress.Executable = false;
                var address = await App.Current.WalletRpcClient.NextExternalAddressAsync(_account);
                CreatedAddress = address;
            }
            catch (Exception ex)
            {
                MessageBox.Show($"Error occurred when requesting address:\n\n{ex}", "Error");
            }
            finally
            {
                RequestAddress.Executable = true;
                CommandManager.InvalidateRequerySuggested();
            }
        }
        #endregion
    }
}
