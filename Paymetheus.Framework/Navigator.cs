// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Windows.Controls;

namespace Paymetheus.Framework
{
    public class Navigator
    {
        private static Navigator _instance;
        private Frame _frame;
        private Navigator(Frame frame)
        {
            _frame = frame;
        }

        public static Navigator GetInstance(Frame frame = null)
        {
            if (_instance == null)
                _instance = new Navigator(frame);
            return _instance;
        }
        
        public void NavigateTo(Page page)
        {
            _frame.Content = page;
        }
    }
}
