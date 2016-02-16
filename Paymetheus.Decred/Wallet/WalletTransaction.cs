// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Script;
using System;
using System.Collections.Generic;
using System.Linq;

namespace Paymetheus.Decred.Wallet
{
    /// <summary>
    /// Describes a transaction, plus additional details concerning this wallet.
    /// </summary>
    public sealed class WalletTransaction
    {
        public WalletTransaction(Transaction transaction, Blake256Hash hash, Input[] inputs, Output[] outputs, Amount? fee, DateTimeOffset seenTime)
        {
            if (transaction == null)
                throw new ArgumentNullException(nameof(transaction));
            if (hash == null)
                throw new ArgumentNullException(nameof(hash));
            if (inputs == null)
                throw new ArgumentNullException(nameof(inputs));
            if (outputs == null)
                throw new ArgumentNullException(nameof(outputs));

            Hash = hash;
            Inputs = inputs;
            Outputs = outputs;
            Fee = fee;
            Transaction = transaction;
            SeenTime = seenTime;
        }

        public struct Input
        {
            public Input(Amount amount, Account previousAccount)
            {
                Amount = amount;
                PreviousAccount = previousAccount;
            }

            public Amount Amount { get; }
            public Account PreviousAccount { get; }
        }

        public class Output
        {
            private Output(Amount amount)
            {
                Amount = amount;
            }

            public Amount Amount { get; }

            public sealed class ControlledOutput : Output
            {
                public ControlledOutput(Amount amount, Account account, bool change) : base(amount)
                {
                    Account = account;
                    Change = change;
                }

                public Account Account { get; }
                public bool Change { get; }
            }

            public sealed class UncontrolledOutput : Output
            {
                public UncontrolledOutput(Amount amount, byte[] pkScript) : base(amount)
                {
                    if (pkScript == null)
                        throw new ArgumentNullException(nameof(pkScript));

                    PkScript = OutputScript.ParseScript(pkScript);
                }

                public OutputScript PkScript { get; }
            }
        }

        public Transaction Transaction { get; }
        public Blake256Hash Hash { get; }
        public Input[] Inputs { get; }
        public Output[] Outputs { get; }
        public Amount? Fee { get; }
        public DateTimeOffset SeenTime { get; }

        public IEnumerable<Output> NonChangeOutputs => Outputs.Where(o =>
        {
            var controlledOutput = o as Output.ControlledOutput;
            return controlledOutput == null || !controlledOutput.Change;
        });
    }
}
