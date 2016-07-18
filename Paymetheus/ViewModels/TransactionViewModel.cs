// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using System;
using System.Collections.Generic;
using System.Linq;

namespace Paymetheus.ViewModels
{
    public sealed class TransactionViewModel : ViewModelBase
    {
        public TransactionViewModel(Wallet wallet, WalletTransaction transaction, BlockIdentity transactionLocation)
        {
            _transaction = transaction;
            _location = transactionLocation;

            var groupedOutputs = _transaction.NonChangeOutputs.Select(o =>
            {
                var destination = wallet.OutputDestination(o);
                return new GroupedOutput(o.Amount, destination);
            }).Aggregate(new List<GroupedOutput>(), (items, next) =>
            {
                var item = items.Find(a => a.Destination == next.Destination);
                if (item == null)
                    items.Add(next);
                else
                    item.Amount += next.Amount;

                return items;
            });

            Depth = BlockChain.Depth(wallet.ChainTip.Height, transactionLocation);
            Inputs = _transaction.Inputs.Select(input => new Input(-input.Amount, wallet.AccountName(input.PreviousAccount))).ToArray();
            Outputs = _transaction.Outputs.Select(output => new Output(output.Amount, wallet.OutputDestination(output))).ToArray();
            GroupedOutputs = groupedOutputs;
        }

        private readonly WalletTransaction _transaction;

        public struct Input
        {
            public Input(Amount amount, string previousAccount)
            {
                Amount = amount;
                PreviousAccount = previousAccount;
            }

            public Amount Amount { get; }
            public string PreviousAccount { get; }
        }

        public struct Output
        {
            public Output(Amount amount, string destination)
            {
                Amount = amount;
                Destination = destination;
            }

            public Amount Amount { get; }
            public string Destination { get; }
        }

        public Blake256Hash TxHash => _transaction.Hash;
        public Input[] Inputs { get; }
        public Output[] Outputs { get; }
        public Amount? Fee => _transaction.Fee;
        public DateTime LocalSeenTime => _transaction.SeenTime.LocalDateTime;

        private BlockIdentity _location;
        public BlockIdentity Location
        {
            get { return _location; }
            internal set { _location = value; RaisePropertyChanged(); }
        }

        private int _depth;
        public int Depth
        {
            get { return _depth; }
            set { _depth = value; RaisePropertyChanged(); RaisePropertyChanged(nameof(ConfirmationStatus)); }
        }

        public ConfirmationStatus ConfirmationStatus
        {
            get
            {
                // TODO: required number of confirmations needs to be configurable
                if (Depth >= 2)
                    return ConfirmationStatus.Confirmed;
                else
                    return ConfirmationStatus.Pending;
            }
        }

        // TODO: The category needs more sophisticated detection for other cateogry types.
        public TransactionCategory Category
        {
            get
            {
                if (Inputs.Length > 0)
                    return TransactionCategory.Send;
                else
                    return TransactionCategory.Receive;
            }
        }

        public Amount DebitCredit
        {
            get
            {
                Amount debitCredit = 0;
                foreach (var controlledInput in _transaction.Inputs)
                    debitCredit += controlledInput.Amount;
                foreach (var controlledOutput in _transaction.Outputs.OfType<WalletTransaction.Output.ControlledOutput>())
                {
                    if (controlledOutput.Change)
                        debitCredit -= controlledOutput.Amount;
                    else
                        debitCredit += controlledOutput.Amount;
                }
                return debitCredit;
            }
        }

        public sealed class GroupedOutput
        {
            public Amount Amount { get; set; }
            public string Destination { get; }
            public GroupedOutput(Amount amount, string destination)
            {
                Amount = amount;
                Destination = destination;
            }
        }

        public List<GroupedOutput> GroupedOutputs { get; }
    }
}
