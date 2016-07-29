// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Grpc.Core;
using Paymetheus.Decred;
using Paymetheus.Decred.Script;
using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    class CreateTransactionViewModel : ViewModelBase
    {
        public CreateTransactionViewModel() : base()
        {
            var synchronizer = ViewModelLocator.SynchronizerViewModel as SynchronizerViewModel;
            if (synchronizer != null)
            {
                SelectedAccount = synchronizer.Accounts[0];
            }

            AddPendingOutputCommand = new DelegateCommand(AddPendingOutput);
            RemovePendingOutputCommand = new DelegateCommand<PendingOutput>(RemovePendingOutput);
            FinishCreateTransaction = new ButtonCommand("SEND", FinishCreateTransactionAction);
            FinishCreateTransaction.Executable = false;

            AddPendingOutput();
        }

        private Transaction _pendingTransaction;
        private Dictionary<Account, OutputScript> _unusedChangeScripts = new Dictionary<Account, OutputScript>();

        private AccountViewModel _selectedAccount;
        public AccountViewModel SelectedAccount
        {
            get { return _selectedAccount; }
            set { _selectedAccount = value; RaisePropertyChanged(); }
        }

        public class PendingOutput
        {
            bool DestinationValid = false; // No default destination
            bool OutputAmountValid = true; // Output amount defaults to zero, which is valid.

            /// <summary>
            /// Checks whether all user-set fields of the pending output are ready to be used
            /// to create a transaction output.
            /// </summary>
            /// <returns>Validity of the pending output</returns>
            public bool IsValid => DestinationValid && OutputAmountValid;

            public enum Kind
            {
                Address,
                Script,
            }

            private Kind _outputKind = Kind.Address;
            public Kind OutputKind
            {
                get { return _outputKind; }
                set { _outputKind = value; RaiseChanged(); }
            }

            private string _destination;
            private Address _destinationAddress;
            private byte[] _destinationScript;
            public string Destination
            {
                get { return _destination; }
                set
                {
                    try
                    {
                        switch (OutputKind)
                        {
                            case Kind.Address:
                                _destinationAddress = Address.Decode(value);
                                if (_destinationAddress.IntendedBlockChain != App.Current.ActiveNetwork)
                                {
                                    throw new ArgumentException("Address is intended for use on another block chain");
                                }
                                break;
                            case Kind.Script:
                                _destinationScript = Hexadecimal.Decode(value);
                                break;
                        }
                        _destination = value;
                        DestinationValid = true;
                    }
                    catch
                    {
                        DestinationValid = false;
                        if (value != "")
                            throw;
                    }
                    finally
                    {
                        RaiseChanged();
                    }
                }
            }

            private Amount _outputAmount;
            public Amount OutputAmount => _outputAmount;
            public string OutputAmountString
            {
                get { return _outputAmount.ToString(); }
                set
                {
                    try
                    {
                        var outputAmount = Denomination.Decred.AmountFromString(value);
                        if (_outputAmount < 0)
                        {
                            throw new ArgumentException("Output amount may not be negative");
                        }
                        _outputAmount = outputAmount;
                        OutputAmountValid = true;
                    }
                    catch
                    {
                        OutputAmountValid = false;
                        throw;
                    }
                    finally
                    {
                        RaiseChanged();
                    }
                }
            }

            public event EventHandler Changed;

            private void RaiseChanged()
            {
                Changed?.Invoke(this, EventArgs.Empty);
            }

            public OutputScript BuildOutputScript()
            {
                if (!DestinationValid)
                {
                    throw new Exception("Unable to build output script from invalid destination");
                }

                switch (OutputKind)
                {
                    case Kind.Address:
                        return _destinationAddress.BuildScript();

                    case Kind.Script:
                        return new OutputScript.Unrecognized(_destinationScript);

                    default:
                        throw new Exception($"Unknown pending output kind {OutputKind}");
                }
            }
        }

        public ObservableCollection<PendingOutput> PendingOutputs { get; } = new ObservableCollection<PendingOutput>();

        private Amount? _estimatedRemainingBalance;
        public Amount? EstimatedRemainingBalance
        {
            get { return _estimatedRemainingBalance; }
            set { _estimatedRemainingBalance = value; RaisePropertyChanged(); }
        }

        private Amount? _estimatedFee;
        public Amount? EstimatedFee
        {
            get { return _estimatedFee; }
            set { _estimatedFee = value; RaisePropertyChanged(); }
        }

        private bool _signChecked = true;
        public bool SignChecked
        {
            get { return _signChecked; }
            set { _signChecked = value; RaisePropertyChanged(); }
        }

        private bool _publishChecked = true;
        public bool PublishChecked
        {
            get { return _publishChecked; }
            set { _publishChecked = value; RaisePropertyChanged(); }
        }

        public ButtonCommand FinishCreateTransaction { get; }

        public ICommand AddPendingOutputCommand { get; }
        public ICommand RemovePendingOutputCommand { get; }

        private async void AddPendingOutput()
        {
            var pendingOutput = new PendingOutput();
            pendingOutput.Changed += PendingOutput_Changed;
            PendingOutputs.Add(pendingOutput);

            try
            {
                await RecalculatePendingTransaction();
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
        }

        private async void RemovePendingOutput(PendingOutput item)
        {
            if (PendingOutputs.Remove(item))
            {
                item.Changed -= PendingOutput_Changed;

                if (PendingOutputs.Count == 0)
                {
                    AddPendingOutput();
                }

                try
                {
                    await RecalculatePendingTransaction();
                }
                catch (Exception ex)
                {
                    MessageBox.Show(ex.Message, "Error");
                }
            }
        }

        private async void PendingOutput_Changed(object sender, EventArgs e)
        {
            try
            {
                await RecalculatePendingTransaction();
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
        }

        private async Task RecalculatePendingTransaction()
        {
            if (PendingOutputs.Count == 0 || PendingOutputs.Any(x => !x.IsValid))
            {
                UnsetPendingTransaction();
                return;
            }

            var walletClient = App.Current.Synchronizer?.WalletRpcClient;

            var outputs = PendingOutputs.Select(po =>
            {
                var amount = po.OutputAmount;
                var script = po.BuildOutputScript().Script;
                return new Transaction.Output(amount, Transaction.Output.LatestPkScriptVersion, script);
            }).ToArray();

            TransactionAuthor.InputSource inputSource = async targetAmount =>
            {
                var inputs = new Transaction.Input[0];
                // TODO: don't hardcode confs
                var funding = await walletClient.FundTransactionAsync(SelectedAccount.Account, targetAmount, 1);
                if (funding.Item2 >= targetAmount)
                {
                    inputs = funding.Item1.Select(o =>
                        Transaction.Input.CreateFromPrefix(new Transaction.OutPoint(o.TransactionHash, o.OutputIndex, o.Tree),
                                                           TransactionRules.MaxInputSequence)).ToArray();
                }
                return Tuple.Create(funding.Item2, inputs);
            };
            TransactionAuthor.ChangeSource changeSource = async () =>
            {
                // Use cached change script if one has already been generated for this account.
                var selectedAccount = SelectedAccount.Account;
                OutputScript cachedScript;
                if (_unusedChangeScripts.TryGetValue(selectedAccount, out cachedScript))
                    return cachedScript;

                var changeAddress = await walletClient.NextInternalAddressAsync(SelectedAccount.Account);
                var changeScript = Address.Decode(changeAddress).BuildScript();
                _unusedChangeScripts[selectedAccount] = changeScript;
                return changeScript;
            };

            try
            {
                var r = await TransactionAuthor.BuildUnsignedTransaction(outputs, TransactionFees.DefaultFeePerKb, inputSource, changeSource);
                Amount totalAccountBalance;
                using (var walletGuard = await App.Current.Synchronizer.WalletMutex.LockAsync())
                {
                    totalAccountBalance = walletGuard.Instance.LookupAccountProperties(SelectedAccount.Account).TotalBalance;
                }
                SetPendingTransaction(totalAccountBalance, r.Item1, r.Item2, outputs.Sum(o => o.Amount));
            }
            catch (Exception ex)
            {
                UnsetPendingTransaction();

                // Insufficient funds will need a nicer error displayed somehow.  For now, hide it
                // while disabling the UI to create the transaction.  All other errors are unexpected.
                if (!(ex is InsufficientFundsException)) throw;
            }
        }

        private void UnsetPendingTransaction()
        {
            _pendingTransaction = null;
            EstimatedFee = null;
            EstimatedRemainingBalance = null;
            FinishCreateTransaction.Executable = false;
        }

        private void SetPendingTransaction(Amount totalAccountBalance, Transaction unsignedTransaction, Amount inputAmount, Amount targetOutput)
        {
            var actualFee = TransactionFees.ActualFee(unsignedTransaction, inputAmount);

            _pendingTransaction = unsignedTransaction;

            EstimatedFee = actualFee;
            EstimatedRemainingBalance = totalAccountBalance - targetOutput - actualFee;
            FinishCreateTransaction.Executable = true;
        }

        private void FinishCreateTransactionAction()
        {
            try
            {
                if (SignChecked)
                {
                    SignTransaction(PublishChecked);
                }
                else
                {
                    throw new NotImplementedException();
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message);
            }
        }

        private void SignTransaction(bool publish)
        {
            var outputs = PendingOutputs.Select(po =>
            {
                var script = po.BuildOutputScript().Script;
                return new Transaction.Output(po.OutputAmount, Transaction.Output.LatestPkScriptVersion, script);
            }).ToArray();
            var shell = ViewModelLocator.ShellViewModel as ShellViewModel;
            if (shell != null)
            {
                Func<string, Task<bool>> action =
                    passphrase => SignTransactionWithPassphrase(passphrase, _pendingTransaction, publish);
                shell.VisibleDialogContent = new PassphraseDialogViewModel(shell, "Enter passphrase to sign transaction", "Sign", action);
            }
        }

        private async Task<bool> SignTransactionWithPassphrase(string passphrase, Transaction tx, bool publishImmediately)
        {
            Tuple<Transaction, bool> signingResponse;
            var walletClient = App.Current.Synchronizer.WalletRpcClient;
            try
            {
                signingResponse = await walletClient.SignTransactionAsync(passphrase, tx);
            }
            catch (RpcException ex) when (ex.Status.StatusCode == StatusCode.InvalidArgument)
            {
                MessageBox.Show(ex.Status.Detail);
                return false;
            }
            var complete = signingResponse.Item2;
            if (!complete)
            {
                MessageBox.Show("Failed to create transaction input signatures.");
                return false;
            }
            var signedTransaction = signingResponse.Item1;

            if (!publishImmediately)
            {
                MessageBox.Show("Reviewing signed transaction before publishing is not implemented yet.");
                return false;
            }

            var serializedTransaction = signedTransaction.Serialize();
            var publishedTxHash = await walletClient.PublishTransactionAsync(signedTransaction.Serialize());

            _pendingTransaction = null;
            _unusedChangeScripts.Remove(SelectedAccount.Account);
            PendingOutputs.Clear();
            AddPendingOutput();
            PublishedTxHash = publishedTxHash.ToString();

            return true;
        }

        private string _publishedTxHash = "";
        public string PublishedTxHash
        {
            get { return _publishedTxHash; }
            set { _publishedTxHash = value;  RaisePropertyChanged(); }
        }
    }
}
