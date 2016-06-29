// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Script;
using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using System;
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
            FinishCreateTransaction = new ButtonCommand("Send", FinishCreateTransactionAction);
            FinishCreateTransaction.Executable = false;

            AddPendingOutput();
        }

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

        private void AddPendingOutput()
        {
            var pendingOutput = new PendingOutput();
            pendingOutput.Changed += PendingOutput_Changed;
            PendingOutputs.Add(pendingOutput);
            RecalculateTransaction();
        }

        private void RemovePendingOutput(PendingOutput item)
        {
            if (PendingOutputs.Remove(item))
            {
                item.Changed -= PendingOutput_Changed;

                if (PendingOutputs.Count == 0)
                {
                    AddPendingOutput();
                }

                RecalculateTransaction();
            }
        }

        private void PendingOutput_Changed(object sender, EventArgs e)
        {
            RecalculateTransaction();
        }

        private void RecalculateTransaction()
        {
            if (PendingOutputs.Count > 0 && PendingOutputs.All(x => x.IsValid))
            {
                // TODO: calculate estimated fee
                EstimatedFee = 0;
                EstimatedRemainingBalance = 0;

                // TODO: only make executable if we know the transaction can be created
                FinishCreateTransaction.Executable = true;
            }
            else
            {
                EstimatedFee = null;
                EstimatedRemainingBalance = null;
                FinishCreateTransaction.Executable = false;
            }
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
                Func<string, Task> action = (passphrase) => SignTransactionWithPassphrase(passphrase, outputs, publish);
                shell.VisibleDialogContent = new PassphraseDialogViewModel(shell, "Enter passphrase to sign transaction", "Sign", action);
            }
        }

        private async Task SignTransactionWithPassphrase(string passphrase, Transaction.Output[] outputs, bool publishImmediately)
        {
            var walletClient = App.Current.Synchronizer.WalletRpcClient;
            var requiredConfirmations = 1; // TODO: Don't hardcode confs.
            var targetAmount = outputs.Sum(o => o.Amount);
            var targetFee = (Amount)1e6; // TODO: Don't hardcode fee/kB.
            var funding = await walletClient.FundTransactionAsync(SelectedAccount.Account, targetAmount + targetFee, requiredConfirmations);
            var fundingAmount = funding.Item2;
            if (fundingAmount < targetAmount + targetFee)
            {
                MessageBox.Show($"Transaction requires {(Amount)(targetAmount + targetFee)} input value but only {fundingAmount} is spendable.",
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
    }
}
