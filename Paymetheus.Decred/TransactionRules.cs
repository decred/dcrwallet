// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;

namespace Paymetheus.Decred
{
    public static class TransactionRules
    {
        public const uint MaxInputSequence = uint.MaxValue;

        // TODO: MaxPayload isn't specific to transactions and should be moved.
        public const int MaxPayload = 1024 * 1024 * 32;
        public const int MaxInputs = MaxPayload / (9 + Blake256Hash.Length) + 1;
        public const int MaxOutputs = MaxPayload / 9 + 1;

        public static readonly Amount MaxOutputValue = (long)21e6 * (long)1e8;

        /// <summary>
        /// Perform a preliminary check that the Amount is within the bounds of valid
        /// transaction output values.  Additional checks are required before authoring
        /// a valid transaction to ensure the total output amount does not exceed the
        /// total previous output amount referenced by the inputs.
        /// </summary>
        public static bool IsSaneOutputValue(Amount a) => a >= 0 && a <= MaxOutputValue;

        public static void CheckSanity(Transaction tx)
        {
            if (tx == null)
                throw new ArgumentNullException(nameof(tx));

            CheckHasInputsAndOutputs(tx);

            // TODO: check serialize size

            CheckOutputValueSanity(tx);

            if (BlockChain.IsCoinbase(tx))
            {
                CheckCoinbaseSignatureScript(tx.Inputs[0]);
            }
            else
            {
                CheckNonCoinbaseInputs(tx);
            }
        }

        private static void CheckHasInputsAndOutputs(Transaction tx)
        {
            if (tx.Inputs.Length == 0)
            {
                throw new TransactionRuleException("Transaction must have at least one input");
            }
            if (tx.Outputs.Length == 0)
            {
                throw new TransactionRuleException("Transaction must have at least one output");
            }
        }

        private static void CheckOutputValueSanity(Transaction tx)
        {
            Amount outputSum = 0;
            int outputIndex = 0;
            foreach (var output in tx.Outputs)
            {
                if (!IsSaneOutputValue(output.Amount))
                {
                    throw new TransactionRuleException($"Output value {output.Amount} for output {outputIndex} is outside valid range");
                }
                if (outputSum - MaxOutputValue + output.Amount > 0)
                {
                    throw new TransactionRuleException("Total output value exceeds maximum");
                }
                outputSum += output.Amount;
                outputIndex++;
            }
        }

        private static void CheckCoinbaseSignatureScript(Transaction.Input coinbaseInput)
        {
            var scriptLength = coinbaseInput.SignatureScript.Length;
            if (scriptLength < BlockChain.MinCoinbaseScriptLength || scriptLength > BlockChain.MaxCoinbaseScriptLength)
            {
                throw new TransactionRuleException($"Coinbase transaction input signature script length {scriptLength} is outside valid range");
            }
        }

        private static void CheckNonCoinbaseInputs(Transaction tx)
        {
            var seenOutPoints = new HashSet<Transaction.OutPoint>();
            foreach (var input in tx.Inputs)
            {
                if (seenOutPoints.Contains(input.PreviousOutpoint))
                {
                    throw new TransactionRuleException($"Transaction input contains duplicate previous output {input.PreviousOutpoint}");
                }
                seenOutPoints.Add(input.PreviousOutpoint);

                if (input.PreviousOutpoint.IsNull())
                {
                    throw new TransactionRuleException("Non-coinbase transaction may not refer to a null previous output");
                }
            }
        }
    }

    public class TransactionRuleException : Exception
    {
        public TransactionRuleException(string message) : base(message) { }
    }
}
